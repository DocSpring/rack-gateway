package audit

import (
	"os"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

func writeAuditLine(data []byte) {
	if len(data) == 0 {
		data = []byte("{}")
	}
	buf := make([]byte, len(data)+1)
	copy(buf, data)
	buf[len(data)] = '\n'
	if _, err := os.Stdout.Write(buf); err != nil {
		gtwlog.Errorf("audit: failed to write audit log line: %v", err)
	}
}
