// Package curl reads pasted cURL commands into the requests a scenario can be
// built from.
//
// What is pasted is text from a person's clipboard, and it is treated as data
// only. Nothing is run, no shell is involved, no variable is expanded, no file
// named in a command is opened and no address in it is fetched. The text is cut
// into words the way a shell would cut it, and the words are read as curl's
// options; whatever has no equivalent in a scenario is reported, not dropped.
package curl
