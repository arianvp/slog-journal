package slogjournal

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"log/syslog"
	"os"
	"path"
	"runtime"
	"slices"
	"strconv"
)

const (
	LevelNotice slog.Level = 1

	LevelCritical  slog.Level = slog.LevelError + 1
	LevelAlert     slog.Level = slog.LevelError + 2
	LevelEmergency slog.Level = slog.LevelError + 3
)

func levelToPriority(l slog.Level) syslog.Priority {
	switch l {
	case slog.LevelDebug:
		return syslog.LOG_DEBUG
	case slog.LevelInfo:
		return syslog.LOG_INFO
	case LevelNotice:
		return syslog.LOG_NOTICE
	case slog.LevelWarn:
		return syslog.LOG_WARNING
	case slog.LevelError:
		return syslog.LOG_ERR
	case LevelCritical:
		return syslog.LOG_CRIT
	case LevelAlert:
		return syslog.LOG_ALERT
	case LevelEmergency:
		return syslog.LOG_EMERG
	default:
		return syslog.LOG_INFO
	}
}

type Options struct {
	Level slog.Leveler

	// ReplaceAttr is called on all non-builtin Attrs before they are written.
	// This can be useful for processing attributes to be in the correct format
	// for log statements outside of your own code as the journal only accepts
	// variables that are uppercase and consist only of characters, numbers and
	// underscores, and may not begin with an underscore.
	ReplaceAttr func(groups []string, a slog.Attr) slog.Attr

	// ReplaceGroup is called on all group names before they are written.  This
	// can be useful for processing group names to be in the correct format for
	// log statements outside of your own code as the journal only accepts
	// variables that are uppercase and consist only of characters, numbers and
	// underscores, and may not begin with an underscore.
	ReplaceGroup func(group string) string
}

// Handler sends logs to the systemd journal.
// variable names must be in uppercase and consist only of characters, numbers and underscores, and may not begin with an underscore.
type Handler struct {
	opts Options
	// NOTE: We only do single Write() calls. Either the message fits in a
	// single datagram, or we send a file descriptor pointing to a tempfd. This
	// makes writes atomic and thus we do not need any additional
	// synchronization.
	w            io.Writer
	groups       []string
	prefix       string
	preformatted []byte
}

const sndBufSize = 8 * 1024 * 1024

func NewHandler(opts *Options) (*Handler, error) {
	h := &Handler{}

	if opts != nil {
		h.opts = *opts
	}

	if h.opts.Level == nil {
		// TODO: Implement a leveler that checks DEBUG_INVOCATION=1
		h.opts.Level = slog.LevelInfo
	}

	w, err := newJournalWriter()
	if err != nil {
		return nil, err
	}

	h.w = w

	return h, nil

}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

var identifier = []byte(path.Base(os.Args[0]))

// Handle handles the Record.
// It will only be called when Enabled returns true.
// The Context argument is as for Enabled.
// It is present solely to provide Handlers access to the context's values.
// Canceling the context should not affect record processing.
// (Among other things, log messages may be necessary to debug a
// cancellation-related problem.)
//
// Handle methods that produce output should observe the following rules:
//   - If r.Time is the zero time, ignore the time.
//   - If r.PC is zero, ignore it.
//   - Attr's values should be resolved.
//   - If an Attr's key and value are both the zero value, ignore the Attr.
//     This can be tested with attr.Equal(Attr{}).
//   - If a group's key is empty, inline the group's Attrs.
//   - If a group has no Attrs (even if it has a non-empty key),
//     ignore it.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	buf := make([]byte, 0, 1024)
	buf = h.appendKV(buf, "MESSAGE", []byte(r.Message))
	buf = h.appendKV(buf, "PRIORITY", []byte(strconv.Itoa(int(levelToPriority(r.Level)))))
	// If r.PC is zero, ignore it.
	if r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		buf = h.appendKV(buf, "CODE_FILE", []byte(f.File))
		buf = h.appendKV(buf, "CODE_FUNC", []byte(f.Function))
		buf = h.appendKV(buf, "CODE_LINE", []byte(strconv.Itoa(f.Line)))
	}

	// If r.Time is the zero time, ignore the time.
	// NOTE: journald does its own timestamping. Lets just ignore
	// NOTE: slogtest requires this. grrr
	if !r.Time.IsZero() {
		timestampStr := strconv.FormatInt(r.Time.UnixMicro(), 10)
		buf = h.appendKV(buf, "SYSLOG_TIMESTAMP", []byte(timestampStr))
	}

	buf = h.appendKV(buf, "SYSLOG_IDENTIFIER", identifier)

	buf = append(buf, h.preformatted...)

	r.Attrs(func(a slog.Attr) bool {
		buf = h.appendAttr(buf, h.prefix, a)
		return true
	})

	_, err := h.w.Write(buf)
	return err

}

func (h *Handler) appendKV(b []byte, k string, v []byte) []byte {
	if bytes.IndexByte(v, '\n') != -1 {
		b = append(b, k...)
		b = append(b, '\n')
		b = binary.LittleEndian.AppendUint64(b, uint64(len(v)))
		b = append(b, v...)
	} else {
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, v...)
		b = append(b, '\n')
	}
	return b
}

// appendAttr has the following rules:
//   - Attr's values should be resolved.
//   - If an Attr's key and value are both the zero value, ignore the Attr.
//     This can be tested with attr.Equal(Attr{}).
//   - If a group's key is empty, inline the group's Attrs.
//   - If a group has no Attrs (even if it has a non-empty key),
//     ignore it.
func (h *Handler) appendAttr(b []byte, prefix string, a slog.Attr) []byte {
	// Attr's values should be resolved.
	a.Value = a.Value.Resolve()

	if rep := h.opts.ReplaceAttr; rep != nil && a.Value.Kind() != slog.KindGroup {
		// a.Value is resolved before calling ReplaceAttr, so the user doesn't have to.
		a = rep(h.groups, a)
		// The ReplaceAttr function may return an unresolved Attr.
		a.Value = a.Value.Resolve()
	}

	// If an Attr's key and value are both the zero value, ignore the Attr.
	if a.Equal(slog.Attr{}) {
		return b
	}

	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		// If a group has no Attrs (even if it has a non-empty key), ignore it.
		if len(attrs) == 0 {
			return b
		}
		// If a group's key is not empty, append the group's key as a prefix.
		// Otherwise, if a group's key is empty, inline the group's Attrs.
		if a.Key != "" {
			if rep := h.opts.ReplaceGroup; rep != nil {
				a.Key = rep(a.Key)
			}
			prefix += a.Key + "_"
		}
		for _, a := range attrs {
			b = h.appendAttr(b, prefix, a)
		}
	} else {
		b = h.appendKV(b, prefix+a.Key, []byte(a.Value.String()))
	}
	return b
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	pre := slices.Clone(h2.preformatted)
	for _, a := range attrs {
		pre = h2.appendAttr(pre, h2.prefix, a)
	}
	h2.preformatted = pre
	return &h2
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	if rep := h.opts.ReplaceGroup; rep != nil {
		name = rep(name)
	}
	return &Handler{
		opts:         h.opts,
		w:            h.w,
		groups:       append(slices.Clip(h.groups), name),
		prefix:       h.prefix + name + "_",
		preformatted: h.preformatted,
	}
}

var _ slog.Handler = &Handler{}
