# proto/

This directory is reserved for future `.proto` (Protocol Buffer) service definitions.

## When to add files here

Add `.proto` files when a gRPC interface is introduced alongside the existing REST API.
Keep one service per file, named `<service>.proto` in snake_case.

## Buf

All files in this directory are managed by [Buf](https://buf.build).
Run `buf lint` from the repository root to validate naming conventions.
Run `buf generate` to regenerate Go stubs after editing `.proto` files.

See `buf.yaml` and `buf.gen.yaml` in the project root for configuration.
