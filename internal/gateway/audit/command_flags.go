package audit

import (
	"path"
	"strings"
)

// flagKind is how a program's credential flag carries its credential.
type flagKind int

const (
	// joinedOnly flags carry it joined to them (mysql -psecret). Alone, they ask for it instead.
	joinedOnly flagKind = iota
	// valueNext flags carry it in the next word (redis-cli -a secret), or joined to them.
	valueNext
	// userPassword flags carry user:password (curl -u admin:secret) in the next word or joined to them. Only the part
	// after the colon is the credential.
	userPassword
	// header flags carry a header joined to them (curl -H'Authorization: Bearer ...' or --header=...): a credential
	// header's value is redacted (see redactHeader), and any other header as a command of its own. A header in the
	// next word is redacted as any other word is.
	header
)

var (
	mysqlFlags = map[string]flagKind{"-p": joinedOnly}
	mongoFlags = map[string]flagKind{"-p": valueNext}
)

// credentialFlags are the flags common programs take a credential with that no secret-looking name gives away, by
// the program's name.
var credentialFlags = map[string]map[string]flagKind{
	"mysql": mysqlFlags, "mysqldump": mysqlFlags, "mysqladmin": mysqlFlags, "mysqlimport": mysqlFlags,
	"mysqlcheck": mysqlFlags, "mysqlshow": mysqlFlags, "mysqlpump": mysqlFlags,
	"mariadb": mysqlFlags, "mariadb-dump": mysqlFlags, "mariadb-admin": mysqlFlags,
	"mongo": mongoFlags, "mongosh": mongoFlags, "mongodump": mongoFlags, "mongorestore": mongoFlags,
	"mongoexport": mongoFlags, "mongoimport": mongoFlags,
	"redis-cli": {"-a": valueNext, "--pass": valueNext},
	"sshpass":   {"-p": valueNext},
	"curl": {
		"-u": userPassword, "--user": userPassword, "-U": userPassword, "--proxy-user": userPassword,
		"-H": header, "--header": header, "--proxy-header": header,
	},
}

// programName is the name of the program word runs, when that program has credential flags (see credentialFlags):
// bin/mysql and /usr/bin/mysql are both mysql.
func programName(word string) (string, bool) {
	name := path.Base(strings.Trim(word, `"'`))
	_, ok := credentialFlags[name]
	return name, ok
}

// redactProgramFlag redacts a credential joined to one of program's credential flags, and returns how to redact
// the next word when that's the flag's credential (nil when it isn't). ok reports whether word was such a flag.
// A short flag takes its credential joined to it (-psecret), a long one after an = (--user=admin:secret).
func redactProgramFlag(program, word string) (redacted string, next func(string) string, ok bool) {
	bare := strings.Trim(word, `"'`)
	for flag, kind := range credentialFlags[program] {
		if bare == flag {
			return word, kind.nextWord(), true
		}
		joined := flag
		if len(flag) > 2 {
			joined += "="
		}
		if len(bare) > len(joined) && strings.HasPrefix(bare, joined) {
			at := strings.Index(word, joined) + len(joined)
			return word[:at] + kind.redactWord(word[at:]), nil, true
		}
	}
	return word, nil, false
}

// redactWord redacts the credential the flag carries in word, keeping any quotes around it.
func (k flagKind) redactWord(word string) string {
	bare := strings.Trim(word, `"'`)
	switch k {
	case userPassword:
		return strings.Replace(word, bare, redactPassword(bare), 1)
	case header:
		if redacted, ok := redactHeader(bare); ok {
			return strings.Replace(word, bare, redacted, 1)
		}
		return strings.Replace(word, bare, RedactCommand(bare), 1)
	default:
		return redactQuoted(word)
	}
}

// nextWord is how to redact the word after the flag alone, or nil when the flag alone carries no credential.
func (k flagKind) nextWord() func(string) string {
	if k == joinedOnly || k == header {
		return nil
	}
	return k.redactWord
}

// redactPassword redacts the password in user:password. Without a colon there's no password: the program asks for
// it.
func redactPassword(value string) string {
	user, _, ok := strings.Cut(value, ":")
	if !ok {
		return value
	}
	return user + ":" + redactedValue
}
