# slog: Systemd journal handler

A [slog](https://pkg.go.dev/log/slog) handler implementing the Systemd [Native
Journal Protocol](https://systemd.io/JOURNAL_NATIVE_PROTOCOL/).

## Usage

```go
var h slog.Handler
if slogjournal.StderrIsJournal() {
    var err error
    h, err = slogjournal.NewHandler(nil)
    if err != nil {
        log.Fatalf("Failed to create journald handler: %v", err)
    }
} else {
    h = slog.NewTextHandler(os.Stderr, nil)
}
log := slog.New(h)
log.Info("Hello, world!", "EXTRA_KEY", "5")
log.Info("Hello, world!", slog.Group("HTTP", "METHOD", "PUT", "URL", "http://example.com"))
```

> [!NOTE]
> Journald only supports keys of the form `^[A-Z_][A-Z0-9_]*$`. Any other keys will be silently dropped.

If you are using another `slog` middleware that produces keys that are not valid for journald,
you can use the `ReplaceAttr` and `ReplaceGroup` helpers.

```go
h := slogjournal.NewHandler(slogjournal.Options{
    ReplaceAttr: func(k string) string {

    },
    ReplaceGroup: slogjournal.ReplaceGroup,
})
```


> [!CAUTION]
> This is pre-release software. No releases have been tagged yet.