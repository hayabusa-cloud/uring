# Copilot Instructions for uring

## Architecture

uring provides a minimal io_uring interface for Linux 6.18+.

### Context Modes
- Direct (0B): Inline 64-bit encoding for simple ops
- Indirect (64B): Pooled context with callback
- Extended (128B): Full SQE copy + user data

## Domain Patterns

### Zero-Allocation Hot Paths
- No defer in submission/completion paths
- Use stack arrays, not make()
- Pool reuse for indirect/extended contexts

### Syscall Integration
- All kernel calls through zcall (zero-overhead)
- Never use syscall or golang.org/x/sys/unix

### Error Handling
- ErrWouldBlock and ErrMore are control flow, not failures
- Use IsSemantic(), IsWouldBlock(), IsMore() for classification
