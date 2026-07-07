package railwaycli

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"railway-tui/internal/dbg"
	"railway-tui/internal/model"
)

// LogStream is a long-lived `railway logs -f --json` subprocess for one source.
// Lines are decoded and pushed onto Lines; process exit closes Lines and the
// final error (if any) is available via Err after Lines drains.
type LogStream struct {
	Source model.Source

	cmd   *exec.Cmd
	Lines chan model.LogLine
	errMu chan error // buffered(1); holds terminal error
}

// StartLogStream launches a streaming logs subprocess for the given source.
// The caller cancels via ctx and drains Lines.
func (c *Client) StartLogStream(ctx context.Context, src model.Source, project string) (*LogStream, error) {
	args := []string{"logs", "--json"}
	switch src.Kind {
	case model.LogBuild:
		args = append(args, "--build")
	case model.LogDeploy:
		args = append(args, "--deployment")
	case model.LogHTTP:
		args = append(args, "--http")
	case model.LogNetwork:
		args = append(args, "--network")
	}
	// --latest keeps us streaming even if the newest deploy is building/failed.
	args = append(args, "--latest")
	args = appendScope(args, project, src.Environment, src.ServiceName)

	dbg.Logf("stream START [%s]: railway %s", src.Key(), strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, c.bin(), args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		dbg.Logf("stream PIPE ERR [%s]: %v", src.Key(), err)
		return nil, err
	}
	var errb strings.Builder
	cmd.Stderr = &lineCollector{b: &errb}
	if err := cmd.Start(); err != nil {
		dbg.Logf("stream START ERR [%s]: %v", src.Key(), err)
		return nil, err
	}

	ls := &LogStream{
		Source: src,
		cmd:    cmd,
		Lines:  make(chan model.LogLine, 256),
		errMu:  make(chan error, 1),
	}

	go func() {
		defer close(ls.Lines)
		n := 0
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			n++
			if n == 1 {
				dbg.Logf("stream FIRST LINE [%s]", src.Key())
			}
			ll := decodeLogLine(line, src)
			select {
			case ls.Lines <- ll:
			case <-ctx.Done():
				dbg.Logf("stream CANCELLED [%s] after %d lines", src.Key(), n)
				return
			}
		}
		waitErr := cmd.Wait()
		stderr := strings.TrimSpace(errb.String())
		dbg.Logf("stream ENDED [%s]: %d lines, err=%v stderr=%q", src.Key(), n, waitErr, stderr)
		select {
		case ls.errMu <- waitErr:
		default:
		}
	}()

	return ls, nil
}

// lineCollector captures a bounded amount of a subprocess's stderr for logging.
type lineCollector struct{ b *strings.Builder }

func (c *lineCollector) Write(p []byte) (int, error) {
	if c.b.Len() < 2048 {
		c.b.Write(p)
	}
	return len(p), nil
}

// Err returns the terminal error after Lines has closed (nil if clean exit or
// still running).
func (ls *LogStream) Err() error {
	select {
	case e := <-ls.errMu:
		return e
	default:
		return nil
	}
}

// decodeLogLine parses one JSON log line. Non-JSON lines (rare, e.g. CLI
// banners) are wrapped as a plain message so nothing is silently dropped.
func decodeLogLine(line string, src model.Source) model.LogLine {
	var generic map[string]any
	if err := json.Unmarshal([]byte(line), &generic); err != nil {
		return model.LogLine{Source: src, Message: line}
	}
	ll := model.LogLine{Source: src, Attrs: generic}
	if v, ok := generic["timestamp"].(string); ok {
		ll.Timestamp = parseTime(v)
	}
	if v, ok := generic["level"].(string); ok {
		ll.Level = v
	}
	if v, ok := generic["message"].(string); ok {
		ll.Message = v
	}
	// HTTP logs have no "message"; synthesize a readable line.
	if ll.Message == "" && src.Kind == model.LogHTTP {
		ll.Message = httpSummary(generic)
	}
	if ll.Message == "" && src.Kind == model.LogNetwork {
		ll.Message = netSummary(generic)
	}
	return ll
}

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func num(m map[string]any, k string) float64 {
	if v, ok := m[k].(float64); ok {
		return v
	}
	return 0
}

func httpSummary(m map[string]any) string {
	var b strings.Builder
	if v := str(m, "method"); v != "" {
		b.WriteString(v)
		b.WriteByte(' ')
	}
	if v := str(m, "path"); v != "" {
		b.WriteString(v)
		b.WriteByte(' ')
	}
	if s := num(m, "httpStatus"); s > 0 {
		b.WriteString(itoa(int(s)))
		b.WriteByte(' ')
	}
	if d := num(m, "totalDuration"); d > 0 {
		b.WriteString(itoa(int(d)))
		b.WriteString("ms")
	}
	if b.Len() == 0 {
		return "(http)"
	}
	return strings.TrimSpace(b.String())
}

func netSummary(m map[string]any) string {
	proto := str(m, "protocol")
	dir := str(m, "direction")
	peer := str(m, "peer")
	s := strings.TrimSpace(proto + " " + dir + " " + peer)
	if s == "" {
		return "(net)"
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
