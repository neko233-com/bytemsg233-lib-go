# bytemsg233-lib-go

Go runtime helpers for bytemsg233 generated code.

## Install

```bash
go get github.com/neko233-com/bytemsg233-lib-go
```

Copy-based install from the main repository:

```bash
bytemsg233 install-lib go --to ./third_party/bytemsg233
```

## API

- `Writer`: field header and scalar writing helpers.
- `Reader`: field header and scalar reading helpers.
- `Pool[T]`: generated model object pooling.
- `EnumFromValue`: small enum restore helper for generated code.
