# File Descriptors and Registered Slots

A process fd names an entry in the process descriptor table. A fixed-file index names a slot on one ring. The `bufIndex` used by `ReadFixed` and `WriteFixed` selects registered memory independently of the file. [File and buffer APIs](../../interface_linux.go)

## Fixed-File Lookup

`RegisterFiles` takes a reference to each supplied file. Once registration succeeds, closing the original process fd leaves that reference in place. `RegisterFilesSparse` starts with empty slots. `RegisteredFileCount` reports the size of a table created by these methods, including empty slots. [Linux registration](https://github.com/torvalds/linux/blob/v6.18/io_uring/rsrc.c#L561-L601)

For a read or poll with `IOSQE_FIXED_FILE`, `CQEView.FD()` returns the submitted slot. Linux holds the file it finds at lookup across later replacements. For example, a read linked behind a blocked poll can reach a replacement installed after both SQEs were consumed. If the request must use a particular file, keep that binding through its matched terminal CQE unless you have earlier evidence of acquisition. [Linux lookup](https://github.com/torvalds/linux/blob/v6.18/io_uring/io_uring.c#L1998-L2015) · [Linked-read test](../../registration_contract_linux_test.go#L143)

## Updating Slots

`RegisterFilesUpdate` installs process fds starting at the given offset. Use `-1` to clear a slot. The returned count covers the processed prefix and can be short with a nil error. If Linux clears the next slot and encounters `EBADF` or `ENOMEM` acquiring the replacement, that slot stays empty, even on a first-entry failure. Later slots keep their files. [Linux update](https://github.com/torvalds/linux/blob/v6.18/io_uring/rsrc.c#L224-L287) · [Partial-update test](../../registration_contract_linux_test.go#L105)

`RegisterFilesUpdate` returns synchronously. The SQE operation `FilesUpdate` can write allocated indexes back to its input array. [Array lifetimes](resources.md#registration) · [SQE fields](encoding.md#direct-files)

### Retirement Tags

The Go registration methods use zero file tags. If you supply a nonzero tag through the Linux ABI, a matching retirement CQE records the release of the last reference to that registration. That CQE can arrive while an earlier submission still awaits file lookup. [Linux resource release](https://github.com/torvalds/linux/blob/v6.18/io_uring/rsrc.c#L512-L529) · [Completion dispatch](sqe-cqe.md#completion-origin)

## Accepted Sockets

| Matched accept | Successful `Res` | Caller owns |
|----------------|------------------|-------------|
| Ordinary accept, including multishot | Process fd, including zero | One socket to transfer or close |
| `AcceptDirect` with an explicit slot | Zero | The socket in the requested slot |
| `AcceptDirect` or `SubmitAcceptDirectMultishot` with `IORING_FILE_INDEX_ALLOC` | Allocated slot index | The socket in that ring slot |

Give every success an owner, including a final CQE and any accept reaped after application delivery stops. A successful cancellation can also return zero. Match the original accept before acting. A socket already transferred stays with its recipient. [Linux accept](https://github.com/torvalds/linux/blob/v6.18/io_uring/net.c#L1679-L1703) · [liburing accept manual](https://man7.org/linux/man-pages/man3/io_uring_prep_accept.3.html) · [Withdrawing an accept handler](../recipes/stream.md#cancel-and-retire)

## Send File Descriptors on a Unix Stream

To pass `SCM_RIGHTS` with `SendMsgHeader` on a Linux `AF_UNIX` stream socket, include at least one payload byte. Keep the listed process fds bound to their files and the header and its storage immutable through the send CQE. Enable `NotifySucceed` and leave `IOSQE_CQE_SKIP_SUCCESS` clear to observe the byte count. A full SQ returns `iox.ErrWouldBlock` before admission, so the original payload and control are still pending. [Send contract](../../interface_linux.go#L924-L939) · [Linux descriptor lookup](https://github.com/torvalds/linux/blob/v6.18/net/core/scm.c#L103-L125) · [Unix socket manual](https://man7.org/linux/man-pages/man7/unix.7.html)

A matched positive `Res` means Linux queued the rights with the first bytes. After a short send, advance the iovecs by `Res` and omit those rights from the suffix. If they were the only control message, set `Control = nil` and `Controllen = 0` on the next header. Reattaching them can give the peer a second set of fds. If the protocol needs confirmation of receipt, have the peer acknowledge it. [Linux stream send](https://github.com/torvalds/linux/blob/v6.18/net/unix/af_unix.c#L2436-L2467) · [Short send and linked NOP](../../sendmsg_header_linux_test.go#L335)

## Receive File Descriptors on a Unix Socket

On a Unix socket, each fd delivered through `SCM_RIGHTS` refers to the same open file description as the sender’s fd. The receiver owns the new descriptor. Leave `IOSQE_CQE_SKIP_SUCCESS` clear on `RecvMsg`. On its matched success, `Res` counts payload bytes and `msg.Controllen` reports the control bytes returned, including any padding. On amd64 and arm64, complete one-fd and two-fd messages can both report 24 control bytes. Read the installed fds with `sock.ParseUnixRights(control[:msg.Controllen])` before reusing the header or control buffer. [Unix socket ancillary data](https://man7.org/linux/man-pages/man7/unix.7.html) · [Control parser](https://github.com/hayabusa-cloud/sock/blob/v0.3.4/scm_rights_linux.go#L87-L121) · [Package round trip](../../sendmsg_header_linux_test.go#L231)

A successful `RecvMsg` on a Linux `AF_UNIX` datagram carrying three fds and no payload reports `Res == 0`. To accept at most one fd, set `msg.Controllen` to `uint64(sock.CmsgLen(4))` (20 bytes). On amd64 and arm64, `sock.CmsgSpace(4)` rounds up to 24 bytes and can fit two fds. In either case Linux sets `MSG_CTRUNC` and drops the undelivered references. If the protocol requires all three, close every installed fd before requesting a new transfer. [Control-length test](../../sendmsg_header_linux_test.go#L529) · [Linux descriptor installation and truncation](https://github.com/torvalds/linux/blob/v6.18/net/core/scm.c#L349-L404) · [Local endpoint handoff](../recipes/protocol.md#hand-off-received-file-descriptors)

## Installing a Process FD

On success, `FixedFdInstall` reports a new process fd for the registered file in `Res`. The fd and the ring slot can be closed independently. To close the slot through `Uring.Close`, pass its index with `IOSQE_FIXED_FILE`. [Installation test](../../integration_linux_test.go#L1493) · [Close encoding](../../operations_linux.go#L430) · [Installation manual](https://github.com/axboe/liburing/blob/liburing-2.13/man/io_uring_prep_fixed_fd_install.3)

## Inter-Ring File Transfers

`MsgRingFD` keeps the source slot registered and installs a reference in the target ring. For an explicit destination, it replaces that slot and success reports zero. With `IORING_FILE_INDEX_ALLOC`, success reports the allocated target index. Both source and target CQEs carry that result when posted. `skipCQE` suppresses only the target CQE. [Transfer tests](../../integration_linux_test.go#L1800)

A source CQE with `-EOVERFLOW` means the file was installed but the target notification failed. In allocation mode, the sender also loses the allocated index from the result. Reconcile the target table before retrying. Another transfer can replace a file or allocate a second slot. [Linux message-ring implementation](https://github.com/torvalds/linux/blob/v6.18/io_uring/msg_ring.c#L176-L205)
