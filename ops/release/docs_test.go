package release

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/cli"
)

// The page that tells somebody how to install and check the tool is read by a person
// who is about to run a binary on a machine that holds a token. What these tests hold in
// place is that it says what is true:
//
//   - the commands it gives to verify a download are the ones a release's own notes give,
//     word for word, and were run by the tests of publish.sh: two copies of a command
//     drift, and the one that is wrong is the one somebody types;
//   - every flag it names exists in the tool, as the tool's own help says;
//   - it names every file a release holds, and says what the sums are not;
//   - every link in it goes somewhere.

// flat is a text with every run of white space made one space: a sentence wrapped across
// two lines is still the sentence.
func flat(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

const (
	cliDoc      = "../../docs/cli.md"
	releasesDoc = "../../docs/releasing.md"
	readmeFile  = "../../README.md"
)

// commandsOf are the indented lines of a markdown file: the commands it shows.
func commandsOf(markdown string) []string {
	var commands []string
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "    ") && strings.TrimSpace(line) != "" {
			commands = append(commands, strings.TrimSpace(line))
		}
	}
	return commands
}

func TestTheDocGivesTheVerificationCommandsOfTheReleaseNotesWordForWord(t *testing.T) {
	inDoc := map[string]bool{}
	for _, command := range commandsOf(readFile(t, cliDoc)) {
		inDoc[command] = true
	}

	// Line for line, and not as a part of a line: a command that is a prefix of the other
	// is a command that does not do what the other does, and "contains" would not see it.
	inNotes := map[string]bool{}
	checked := 0
	for _, command := range commandsOf(readFile(t, "notes.md")) {
		// The download lines carry the tag and the repository of a particular release.
		if strings.Contains(command, "@") {
			continue
		}
		inNotes[command] = true
		checked++
		if !inDoc[command] {
			t.Errorf("the notes tell people to run\n    %s\nand the page does not, exactly", command)
		}
	}
	if checked < 5 {
		t.Fatalf("only %d command(s) were compared: this test would prove nothing", checked)
	}

	// And the other way: a verification command of the page that the notes do not have is
	// one that was changed in one place only.
	for command := range inDoc {
		verifies := strings.HasPrefix(command, "grep ") || strings.HasPrefix(command, "$expected") ||
			strings.HasPrefix(command, "$actual") || strings.HasPrefix(command, "if ($expected")
		if verifies && !inNotes[command] {
			t.Errorf("the page tells people to run\n    %s\nand the release notes do not, exactly", command)
		}
	}
}

func TestEveryFlagTheDocNamesIsOneOfTheTool(t *testing.T) {
	var help, stderr bytes.Buffer
	if code := cli.Main(context.Background(), nil, []string{"help"}, func(string) string { return "" }, &help, &stderr); code != 0 {
		t.Fatalf("help exited %d", code)
	}

	flag := regexp.MustCompile(`--[a-z][a-z-]*`)
	named := flag.FindAllString(readFile(t, cliDoc), -1)
	if len(named) == 0 {
		t.Fatal("the page names no flag: this test would prove nothing")
	}
	for _, f := range named {
		if !strings.Contains(help.String(), f) {
			t.Errorf("the page names %s, which the tool's help does not", f)
		}
	}
}

func TestTheDocNamesEveryFileOfAReleaseAndWhatTheSumsAreNot(t *testing.T) {
	doc := flat(readFile(t, cliDoc))

	for _, name := range append(platformFiles(t), "checksums.txt") {
		if !strings.Contains(doc, name) {
			t.Errorf("the page does not name %s", name)
		}
	}
	if !strings.Contains(strings.ToLower(doc), "not a signature") {
		t.Error("the page does not say that the sums are not a signature")
	}
	// Honest about the one thing a person might think they get.
	if !strings.Contains(strings.ToLower(doc), "genuine") {
		t.Error("the page does not say what the sums cannot show")
	}
}

func TestTheDocSaysWhereTheSettingsComeFromAndWhatEachCodeMeans(t *testing.T) {
	doc := flat(readFile(t, cliDoc))

	for _, want := range []string{
		"BALLAST_URL", "BALLAST_TOKEN", "ballast version", "ballast help",
		"| 0 |", "| 1 |", "| 2 |",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	// The token is never an argument, and the page never shows it as one.
	if strings.Contains(doc, "--token") {
		t.Error("the page shows a --token")
	}
	if strings.Contains(doc, "@TAG@") || strings.Contains(doc, "@REPO@") {
		t.Error("a placeholder of the release notes is in the page")
	}
}

var markdownLink = regexp.MustCompile(`\]\(([^)#\s]+)(#[^)]*)?\)`)

func TestEveryRelativeLinkGoesSomewhere(t *testing.T) {
	for _, file := range []string{cliDoc, releasesDoc, readmeFile} {
		dir := filepath.Dir(file)
		for _, match := range markdownLink.FindAllStringSubmatch(readFile(t, file), -1) {
			target := match[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
				t.Errorf("%s links to %s, which is not there", filepath.Base(file), target)
			}
		}
	}
}

func TestTheReadmeLeadsToThePage(t *testing.T) {
	readme := readFile(t, readmeFile)

	if !strings.Contains(readme, "docs/cli.md") {
		t.Error("the README does not link to docs/cli.md")
	}
	for _, target := range []string{"make build-cli", "make release-cli"} {
		if !strings.Contains(readme, target) {
			t.Errorf("the README's commands do not list %s", target)
		}
	}
}

// The procedure of the maintainer is written down, with the steps that keep a release
// from being a mistake: a trial first, a pre-release before the release, and the setting
// that only the repository can hold.
func TestTheProcedureOfTheMaintainerSaysWhatKeepsAReleaseFromBeingAMistake(t *testing.T) {
	doc := flat(readFile(t, releasesDoc))

	for _, want := range []string{
		"make release-cli", "git tag", "-rc.", "main", "checksums.txt",
		"protection", "brouillon",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the procedure lacks %q", want)
		}
	}
}
