# slog: Systemd journal handler

[![Go Reference](https://pkg.go.dev/badge/github.com/systemd/slog-journal.svg)](https://pkg.go.dev/github.com/systemd/slog-journal)

## Usage

```go
h , err := slogjournal.NewHandler(nil)
log := slog.New(h)
log.Info("Hello, world!", "EXTRA_KEY", "5")
log.Info("Hello, world!", slog.Group("HTTP", "METHOD", "put", "URL", "http://example.com"))
```

### Make sure your logs are compatible with the journal

When using third-party slog libraries, you do not have control over the attributes that are passed to the logger.
Because the journal only supports keys of the form `^[A-Z_][A-Z0-9_]*$`, you may need to transform keys that don't match this pattern.
For this you can use the `ReplaceGroup` and `ReplaceAttr` fields in `Options`:


```go
package main

import (
    "log/slog"
    sloghttp "github.com/samber/slog-http"
    slogjournal "github.com/systemd/slog-journal"
)

func main() {
    h , err := slogjournal.NewHandler(&slogjournal.Options{
        ReplaceGroup: func(k string) string {
            return strings.ReplaceAll(strings.ToUpper(k), "-", "_")
        },
        ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
            a.Key = strings.ReplaceAll(strings.ToUpper(a.Key), "-", "_")
            return a
        },
    })

    log := slog.New(h)
    mux := http.NewServeMux()
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        log.Info("Hello world")
        w.Write([]byte("Hello, world!"))
    })
    http.ListenAndServe(":8080", sloghttp.New(log)(mux))
}
```