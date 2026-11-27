# Sources and Evidence

Use the primary source for the layer the claim concerns. Record its revision, symbol or section and the assumptions needed for the claim. [Evidence requirements](verify.md#evidence)

## Linux and liburing

The guide assumes a maintained stable Linux kernel, version 6.18 or later. The table cites Linux and liburing by release so each contract can be checked at its source.

| Claim | Primary source |
|-------|----------------|
| SQE/CQE layout and flags | [Linux UAPI](https://github.com/torvalds/linux/blob/v6.18/include/uapi/linux/io_uring.h) |
| Request identity and poll ID changes | [user-data manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_sqe_set_data.3), [poll update and rearm](https://github.com/torvalds/linux/blob/v6.18/io_uring/poll.c#L925-L955) |
| Cancellation matching and drain deferral | [cancel.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/cancel.c), [io_uring.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/io_uring.c), [cancellation manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_cancel.3) |
| Timer counts, updates, expiry and linked submission boundaries | [timeout.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/timeout.c), [control CQE results](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_timeout_update.3#L61-L78), [link flags in io_uring_enter(2)](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_enter.2) |
| Setup, normalization, and issuer checks | [io_uring.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/io_uring.c), [internal limits](https://github.com/torvalds/linux/blob/v6.18/io_uring/io_uring.h) |
| Network operation validation and completion | [net.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/net.c) |
| Provided buffers and descriptor consumption order | [kbuf.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/kbuf.c), [receive manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_recv.3) |
| Incremental offsets and buffer retention | [kbuf.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/kbuf.c), [buffer-ring registration](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_register_buf_ring.3) |
| Staging and publishing buffer entries | [buffer-ring add](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_buf_ring_add.3), [advance](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_buf_ring_advance.3) |
| SEND_ZC short results, data, and notification lifetime | [send-ZC manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_send_zc.3), [notification source](https://github.com/torvalds/linux/blob/v6.18/io_uring/notif.c) |
| Read/write offsets and multishot read | [rw.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/rw.c) |
| Iovec counts and import | [uio.h](https://github.com/torvalds/linux/blob/v6.18/include/uapi/linux/uio.h), [iov_iter.c](https://github.com/torvalds/linux/blob/v6.18/lib/iov_iter.c) |
| Epoll fields | [epoll.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/epoll.c) |
| WAITID fields and domains | [waitid.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/waitid.c), [exit.c](https://github.com/torvalds/linux/blob/v6.18/kernel/exit.c) |
| FUTEX_WAITV fields and limits | [futex.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/futex.c), [futex UAPI](https://github.com/torvalds/linux/blob/v6.18/include/uapi/linux/futex.h) |
| Registered-file lookup, updates, retirement tags and fixed-iovec import | [fixed-file lookup](https://github.com/torvalds/linux/blob/v6.18/io_uring/io_uring.c#L1998-L2015), [rsrc.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/rsrc.c) |
| File installation into a process or target ring | [installation manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_fixed_fd_install.3), [msg_ring.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/msg_ring.c#L176-L205) |
| Registration descriptor copyback and memory-region lifetime | [register.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/register.c), [memmap.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/memmap.c) |
| ZCRX setup, references, and refill | [documentation](https://github.com/torvalds/linux/blob/v6.18/Documentation/networking/iou-zcrx.rst), [v6.18 implementation](https://github.com/torvalds/linux/blob/v6.18/io_uring/zcrx.c), [v6.19 comparison](https://github.com/torvalds/linux/blob/v6.19/io_uring/zcrx.c), [liburing example](https://github.com/axboe/liburing/blob/liburing-2.13/examples/zcrx.c) |

For broader ABI context, use [io_uring(7)](https://man7.org/linux/man-pages/man7/io_uring.7.html), [setup](https://man7.org/linux/man-pages/man2/io_uring_setup.2.html), [enter](https://man7.org/linux/man-pages/man2/io_uring_enter.2.html), and [register](https://man7.org/linux/man-pages/man2/io_uring_register.2.html). Resolve a conflict by checking the exact kernel implementation and the applicability of the documented contract.

For ordering claims, read the [kernel memory-barrier documentation](https://docs.kernel.org/core-api/wrappers/memory-barriers.html) and [Linux Kernel Memory Model](https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/tools/memory-model). A userspace compiler or language claim also needs its own model.

## Go and Package Contracts

| Claim | Source |
|-------|--------|
| Language semantics | [Go specification](https://go.dev/ref/spec) |
| Copies and shared storage | [Value representation](https://go.dev/ref/spec#Representation_of_values), [assignment statements](https://go.dev/ref/spec#Assignment_statements) |
| Synchronization and visibility | [Go memory model](https://go.dev/ref/mem) |
| Pointer conversion and reachability | [unsafe.Pointer](https://pkg.go.dev/unsafe#Pointer) |
| Toolchain behavior | [Go source](https://go.googlesource.com/go), [command documentation](https://go.dev/doc/cmd), [release notes](https://go.dev/doc/devel/release) |
| cgo pointer rules, when applicable | [cgo documentation](https://go.dev/cmd/cgo/) |
| Package API and implementation | Read the source at the selected revision. [Package overview](../doc.go) · [Published uring API](https://pkg.go.dev/code.hybscloud.com/uring) |

Trace package behavior through the owning implementation and its consumers. [Package ownership](boundary/integration.md) · [Revision requirements](verify.md#read-sources)

## Proofs

The [proof project](../proofs/README.md) names its model, source mapping, assumptions and checked theorems. Use [Lean's validation guidance](https://lean-lang.org/doc/reference/latest/ValidatingProofs/) for proof checking and axiom review. A checked model needs a separate correspondence argument before it establishes an implementation property.

Connect the source to the applicable [core contract](core.md) and record any assumptions that still need evidence.
