package slogjournal

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strconv"
	"syscall"
)

type Priority int

const (
	priEmerg Priority = iota
	priAlert
	priCrit
	priErr
	priWarning
	priNotice
	priInfo
	priDebug
)

const (
	LevelNotice    slog.Level = 1
	LevelCritical  slog.Level = slog.LevelError + 1
	LevelAlert     slog.Level = slog.LevelError + 2
	LevelEmergency slog.Level = slog.LevelError + 3
)

func levelToPriority(l slog.Level) Priority {
	switch l {
	case slog.LevelDebug:
		return priDebug
	case slog.LevelInfo:
		return priInfo
	case LevelNotice:
		return priNotice
	case slog.LevelWarn:
		return priWarning
	case slog.LevelError:
		return priErr
	case LevelCritical:
		return priCrit
	case LevelAlert:
		return priAlert
	default:
		panic("unreachable")
	}
}

type Options struct {
	Level slog.Leveler
	Addr  string // Address of the journal socket. If not set defaults to /run/systemd/journal/socket. This is useful for testing.
}

type Handler struct {
	opts         Options
	w            io.Writer
	prefix       string
	preformatted *bytes.Buffer
}

const sndBufSize = 8 * 1024 * 1024

func NewHandler(opts *Options) (*Handler, error) {
	h := &Handler{}

	if opts != nil {
		h.opts = *opts
	}

	if h.opts.Level == nil {
		h.opts.Level = slog.LevelInfo
	}

	// The "net" library in Go really wants me to either Dial or Listen a UnixConn,
	// which would respectively bind() an address or connect() to a remote address,
	// but we want neither. We want to create a datagram socket and write to it directly
	// and not worry about reconnecting or rebinding.
	// so jumping through some hoops here
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return nil, err
	}

	if err := syscall.SetNonblock(fd, true); err != nil {
		return nil, err
	}

	f := os.NewFile(uintptr(fd), "journal")
	defer f.Close()

	fconn, err := net.FileConn(f)
	if err != nil {
		return nil, err
	}
	conn, ok := fconn.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("expected *net.UnixConn, got %T", fconn)
	}

	if err := conn.SetWriteBuffer(sndBufSize); err != nil {
		return nil, err
	}

	if h.opts.Addr == "" {
		h.opts.Addr = "/run/systemd/journal/socket"
	}
	addr := &net.UnixAddr{
		Name: h.opts.Addr,
		Net:  "unixgram",
	}

	h.w = &journalWriter{
		conn: conn,
		addr: addr,
	}

	h.preformatted = new(bytes.Buffer)
	h.prefix = ""

	return h, nil

}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

// Handle implements slog.Handler.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	buf := new(bytes.Buffer)
	h.appendKV(buf, "MESSAGE", []byte(r.Message))
	h.appendKV(buf, "PRIORITY", []byte(strconv.Itoa(int(levelToPriority(r.Level)))))
	if r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		h.appendKV(buf, "CODE_FILE", []byte(f.File))
		h.appendKV(buf, "CODE_FUNC", []byte(f.Function))
		h.appendKV(buf, "CODE_LINE", []byte(strconv.Itoa(f.Line)))
	}

	if !r.Time.IsZero() {
		h.appendKV(buf, "TIMESTAMP", []byte(strconv.Itoa(int(r.Time.Unix()))))
	}

	if _, err := buf.ReadFrom(h.preformatted); err != nil {
		return err
	}

	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(buf, h.prefix, a)
		return true
	})

	_, err := h.w.Write(buf.Bytes())
	return err

}

func (h *Handler) appendKV(b *bytes.Buffer, k string, v []byte) {
	if bytes.IndexByte(v, '\n') != -1 {
		b.WriteString(k)
		b.WriteByte('\n')
		binary.Write(b, binary.LittleEndian, uint64(len(v)))
		b.Write(v)
	} else {
		b.WriteString(k)
		b.WriteByte('=')
		b.Write(v)
		b.WriteByte('\n')
	}
}

func (h *Handler) appendAttr(b *bytes.Buffer, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "_"
		}
		for _, g := range a.Value.Group() {
			h.appendAttr(b, prefix, g)
		}
	} else if key := a.Key; key != "" {
		h.appendKV(b, prefix+key, []byte(a.Value.String()))
	}
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	buf := new(bytes.Buffer)
	buf.ReadFrom(h.preformatted)
	for _, a := range attrs {
		h.appendAttr(buf, h.prefix, a)
	}
	return &Handler{
		opts:         h.opts,
		w:            h.w,
		prefix:       h.prefix,
		preformatted: buf,
	}
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		opts:         h.opts,
		w:            h.w,
		prefix:       h.prefix + name + "_",
		preformatted: h.preformatted,
	}
}

var _ slog.Handler = &Handler{}
