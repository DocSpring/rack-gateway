package convox

// AttachedRunPlaceholder is the command `convox run` starts a one-off process with (at its default --timeout) when
// it isn't detached: it then execs the real command into that process and stops it afterwards. The gateway lets
// the placeholder start through without an MFA proof or a deploy approval's command match, since it runs nothing,
// and checks the exec. The CLI sends its proof with the exec, so a run takes one touch.
const AttachedRunPlaceholder = "sleep 3600"
