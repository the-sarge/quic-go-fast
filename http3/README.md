# HTTP/3

[![Documentation](https://img.shields.io/badge/docs-quic--go.net-red?style=flat)](https://quic-go.net/docs/)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/quic-go/quic-go/http3)](https://pkg.go.dev/github.com/quic-go/quic-go/http3)

This package implements HTTP/3 ([RFC 9114](https://datatracker.ietf.org/doc/html/rfc9114)), including QPACK ([RFC 9204](https://datatracker.ietf.org/doc/html/rfc9204)) and HTTP Datagrams ([RFC 9297](https://datatracker.ietf.org/doc/html/rfc9297)).
It aims to provide feature parity with the standard library's HTTP/1.1 and HTTP/2 implementation.

Detailed documentation can be found on [quic-go.net](https://quic-go.net/docs/).

## Error handling

HTTP/3 stream reads, writes (including `TryWriteAll`), datagrams, request headers/trailers, response parsing, request-stream opening, and server request-stream acceptance convert applicable QUIC stream and application failures to `*http3.Error`, possibly wrapped with operation context. Use `errors.As` to inspect the error, and `errors.Is` to match its code and local/remote identity:

```go
var h3err *http3.Error
if errors.As(err, &h3err) {
    log.Printf("HTTP/3 code=%v remote=%t message=%q", h3err.ErrorCode, h3err.Remote, h3err.ErrorMessage)
}
if errors.Is(err, &http3.Error{ErrorCode: http3.ErrCodeRequestCanceled, Remote: true}) {
    // The peer canceled the request.
}
```

These checks replace checks against `*quic.StreamError` or `*quic.ApplicationError` at the affected HTTP/3 APIs. `http3.Error.Is` compares the code and `Remote` flag; it does not compare `ErrorMessage`. The converted error does not retain the original QUIC error or its stream ID. When using a low-level HTTP/3 stream, obtain its ID from `StreamID()` separately. Raw QUIC APIs and connection context causes keep their QUIC error types.

Response-parsing failures returned by `Transport.RoundTrip`, `Transport.RoundTripOpt`, and `ClientConn.RoundTrip` could previously be bare `*http3.Error` values. They can now be wrapped with operation context. Existing callers using a direct type assertion such as `err.(*http3.Error)` should also migrate to `errors.As`; use `errors.Is` to match code and local/remote identity.

`Server.ServeQUICConn` returns contextually wrapped `*http3.Error` values for connection failures while accepting request streams. Failures during control-stream setup retain their contextually wrapped `*quic.ApplicationError` representation. Shutdown classifiers should use `errors.As` for both types, checking the code and local/remote identity appropriate to each path.

Unrelated errors, including EOF, context cancellation, deadlines, transport errors, and `quic.ErrWouldBlock`, retain their existing identity and wrapping behavior. Error conversion does not change request retry rules or when an exchange releases its pooled connection.
