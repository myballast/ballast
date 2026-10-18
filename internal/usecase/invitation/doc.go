// Package invitation lets a project's administrators bring people in who are not
// members yet, and lets those people accept.
//
// No mail is sent. The administrator is shown a link once and passes it on
// themselves; Ballast stores only the hash of its token, so a copy of the
// database is not a set of working links.
package invitation
