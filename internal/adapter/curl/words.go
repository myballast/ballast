package curl

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// word is one argument of a command, once quotes and escapes are resolved.
type word struct {
	text string
	// expands is true when the shell would have replaced something in it: $NAME,
	// ${NAME}, $(command) or a backtick. Nothing is replaced here, and the text
	// keeps the characters as they were written.
	expands bool
}

// command is the words of one command, and whether anything was left behind
// after a pipe or a redirection.
type command struct {
	words []word
	// tail is true when the line went on with a pipe, a redirection or a
	// background marker, whose effect on the output is not a request.
	tail bool
}

// splitCommands cuts a text into commands the way a shell cuts a script: lines
// and ; and && and || separate commands, a backslash before a newline joins two
// lines, and ', " and $' quote. It never runs or expands anything.
func splitCommands(text string) []command {
	cutter := &cutter{input: []rune(text)}
	cutter.run()
	return cutter.commands
}

type cutter struct {
	input    []rune
	position int

	commands []command
	current  command
	// word being built, and whether one is open (an empty '' is still a word).
	text    strings.Builder
	open    bool
	expands bool
	// ignoring is true after a pipe or redirection, until the command ends.
	ignoring bool
}

func (c *cutter) run() {
	for c.position < len(c.input) {
		character := c.input[c.position]

		switch {
		case character == '\\':
			c.backslash()
		case character == '\'':
			c.singleQuoted()
		case character == '"':
			c.doubleQuoted()
		case character == '$' && c.peek(1) == '\'':
			c.ansiQuoted()
		case character == '$' || character == '`':
			c.markExpansion(character)
		case character == '#' && !c.open:
			c.skipComment()
		case character == '\n':
			c.endCommand()
			c.position++
		case character == ';':
			c.endCommand()
			c.position++
		case character == '&' && c.peek(1) == '&', character == '|' && c.peek(1) == '|':
			c.endCommand()
			c.position += 2
		case character == '|', character == '>', character == '<', character == '&':
			c.dropFileDescriptor()
			c.endWord()
			c.ignoring = true
			c.current.tail = true
			c.position++
		case character == ' ' || character == '\t' || character == '\r':
			c.endWord()
			c.position++
		default:
			c.add(character)
			c.position++
		}
	}
	c.endCommand()
}

func (c *cutter) peek(ahead int) rune {
	if c.position+ahead >= len(c.input) {
		return 0
	}
	return c.input[c.position+ahead]
}

func (c *cutter) add(character rune) {
	if c.ignoring {
		return
	}
	c.text.WriteRune(character)
	c.open = true
}

// dropFileDescriptor discards the 2 of "2>&1": a number glued to a redirection
// names a stream, it is not an argument.
func (c *cutter) dropFileDescriptor() {
	if _, err := strconv.Atoi(c.text.String()); err == nil && c.text.Len() > 0 && c.position > 0 && c.input[c.position-1] != ' ' {
		c.text.Reset()
		c.open = false
	}
}

func (c *cutter) endWord() {
	if c.open && !c.ignoring {
		c.current.words = append(c.current.words, word{text: c.text.String(), expands: c.expands})
	}
	c.text.Reset()
	c.open = false
	c.expands = false
}

func (c *cutter) endCommand() {
	c.endWord()
	if len(c.current.words) > 0 || c.current.tail {
		c.commands = append(c.commands, c.current)
	}
	c.current = command{}
	c.ignoring = false
}

// backslash escapes the next character, or joins two lines when a newline
// follows. A caret does the same in the Windows command line.
func (c *cutter) backslash() {
	next := c.peek(1)
	switch next {
	case 0:
		c.position++
	case '\n':
		c.position += 2
	case '\r':
		c.position += 2
		if c.peek(0) == '\n' {
			c.position++
		}
	default:
		c.add(next)
		c.position += 2
	}
}

func (c *cutter) markExpansion(character rune) {
	next := c.peek(1)
	if character == '`' || next == '(' || next == '{' || next == '_' || isLetter(next) || (next >= '0' && next <= '9') {
		c.expands = true
	}
	c.add(character)
	c.position++
}

func isLetter(character rune) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
}

// skipComment drops the rest of the line.
func (c *cutter) skipComment() {
	for c.position < len(c.input) && c.input[c.position] != '\n' {
		c.position++
	}
}

// singleQuoted copies everything up to the next quote.
func (c *cutter) singleQuoted() {
	c.position++
	if !c.ignoring {
		c.open = true
	}
	for c.position < len(c.input) && c.input[c.position] != '\'' {
		c.add(c.input[c.position])
		c.position++
	}
	c.position++
}

// doubleQuoted copies up to the next quote; a backslash escapes only the few
// characters a shell lets it escape inside double quotes.
func (c *cutter) doubleQuoted() {
	c.position++
	if !c.ignoring {
		c.open = true
	}

	for c.position < len(c.input) && c.input[c.position] != '"' {
		character := c.input[c.position]

		switch {
		case character == '\\' && strings.ContainsRune("\"\\$`", c.peek(1)):
			c.add(c.peek(1))
			c.position += 2
		case character == '\\' && c.peek(1) == '\n':
			c.position += 2
		case character == '$' || character == '`':
			c.markExpansion(character)
		default:
			c.add(character)
			c.position++
		}
	}
	c.position++
}

// ansiQuoted reads $'...', the form a browser writes when a body holds a newline
// or a quote: \n, \t, \', \\, \xHH, \uHHHH and the like are resolved.
func (c *cutter) ansiQuoted() {
	c.position += 2
	if !c.ignoring {
		c.open = true
	}

	for c.position < len(c.input) && c.input[c.position] != '\'' {
		character := c.input[c.position]
		if character != '\\' {
			c.add(character)
			c.position++
			continue
		}
		c.escape()
	}
	c.position++
}

// escape resolves one backslash sequence of a $'...' string.
func (c *cutter) escape() {
	next := c.peek(1)
	c.position += 2

	simple := map[rune]rune{'n': '\n', 't': '\t', 'r': '\r', 'a': '\a', 'b': '\b', 'f': '\f', 'v': '\v', 'e': 0x1b,
		'\\': '\\', '\'': '\'', '"': '"', '?': '?'}
	if resolved, known := simple[next]; known {
		c.add(resolved)
		return
	}

	switch next {
	case 'x':
		c.hex(2)
	case 'u':
		c.hex(4)
	case 'U':
		c.hex(8)
	case '0', '1', '2', '3', '4', '5', '6', '7':
		c.octal(next)
	default:
		// An escape the shell does not know stays as written.
		c.add('\\')
		if next != 0 {
			c.add(next)
		}
	}
}

// hex reads up to digits hexadecimal digits and adds the character they name.
func (c *cutter) hex(digits int) {
	end := c.position
	for end < len(c.input) && end-c.position < digits && isHexDigit(c.input[end]) {
		end++
	}
	if end == c.position {
		return
	}
	value, err := strconv.ParseUint(string(c.input[c.position:end]), 16, 32)
	c.position = end
	if err != nil || !utf8.ValidRune(rune(value)) {
		c.add(utf8.RuneError)
		return
	}
	c.add(rune(value))
}

func isHexDigit(character rune) bool {
	return (character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')
}

// octal reads up to three octal digits, the first already read.
func (c *cutter) octal(first rune) {
	value := int(first - '0')
	for count := 1; count < 3 && c.position < len(c.input) && c.input[c.position] >= '0' && c.input[c.position] <= '7'; count++ {
		value = value*8 + int(c.input[c.position]-'0')
		c.position++
	}
	c.add(rune(value))
}
