# Sources and Evidence

Use primary sources to establish the precise claim under review. Record the revision, relevant symbol or section, assumptions, and what the evidence does and does not establish.

## Linux and liburing

The baseline below pins Linux v6.18 and liburing 2.13; the v6.19 ZCRX source is an explicit later comparison. A rolling manual or development tree may describe behavior outside that baseline.

| Claim | Primary source |
|-------|----------------|
| SQE/CQE layout and flags | [Linux UAPI](https://github.com/torvalds/linux/blob/v6.18/include/uapi/linux/io_uring.h) |
| Opaque request/completion identity | [user-data manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_sqe_set_data64.3) |
| Cancellation matching and drain deferral | [cancel.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/cancel.c), [io_uring.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/io_uring.c), [cancellation manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_cancel.3) |
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
| Registered-file updates and fixed-iovec import | [rsrc.c](https://github.com/torvalds/linux/blob/v6.18/io_uring/rsrc.c) |
| ZCRX setup, references, and refill | [documentation](https://github.com/torvalds/linux/blob/v6.18/Documentation/networking/iou-zcrx.rst), [v6.18 implementation](https://github.com/torvalds/linux/blob/v6.18/io_uring/zcrx.c), [v6.19 comparison](https://github.com/torvalds/linux/blob/v6.19/io_uring/zcrx.c), [liburing example](https://github.com/axboe/liburing/blob/liburing-2.13/examples/zcrx.c) |

For broader ABI context, use [io_uring(7)](https://man7.org/linux/man-pages/man7/io_uring.7.html), [setup](https://man7.org/linux/man-pages/man2/io_uring_setup.2.html), [enter](https://man7.org/linux/man-pages/man2/io_uring_enter.2.html), and [register](https://man7.org/linux/man-pages/man2/io_uring_register.2.html). Resolve a conflict by checking the exact kernel implementation and the applicability of the documented contract.

For ordering claims, read the [kernel memory-barrier documentation](https://docs.kernel.org/core-api/wrappers/memory-barriers.html) and [Linux Kernel Memory Model](https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/tools/memory-model). A userspace compiler or language claim also needs its own model.

## Go and Package Contracts

| Claim | Source |
|-------|--------|
| Language semantics | [Go specification](https://go.dev/ref/spec) |
| Copies and shared storage | [Value representation](https://go.dev/ref/spec#Representation_of_values), [assignment](https://go.dev/ref/spec#Assignments) |
| Synchronization and visibility | [Go memory model](https://go.dev/ref/mem) |
| Pointer conversion and reachability | [unsafe.Pointer](https://pkg.go.dev/unsafe#Pointer) |
| Toolchain behavior | [Go source](https://go.googlesource.com/go), [command documentation](https://go.dev/doc/cmd), [release notes](https://go.dev/doc/devel/release) |
| cgo pointer rules, when applicable | [cgo documentation](https://go.dev/cmd/cgo/) |
| Package API and implementation | Current source at the module revision used by the task; [package documentation](https://pkg.go.dev/) for navigation |

Use [integration](boundary/integration.md) to identify the owner package. A guide description is not a substitute for reading the implementation when a change depends on its exact behavior.

## Proofs

The [proof project](../proofs/README.md) names its model, source mapping, assumptions and checked theorems. Use [Lean's validation guidance](https://lean-lang.org/doc/reference/latest/ValidatingProofs/) for proof checking and axiom review. A checked model needs a separate correspondence argument before it establishes an implementation property.

For each external claim, record the owning layer, source revision/section, relevant [contract](core.md), evidence and remaining assumptions. Specifications, source inspection, tests, proofs and measurements establish different claims. Follow [verification](verify.md).
