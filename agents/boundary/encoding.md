# Encoding and Admission

Validate the operation's semantic domain and the destination field width before narrowing, indexing, pointer arithmetic or SQE writes. Start from a zeroed SQE or explicitly overwrite every relevant field; slot reuse must not leak another opcode's values. Use the [pinned kernel sources](../references.md#linux-and-liburing) and current wrapper together.

## Input Domains

| Input | Required domain or distinction |
|-------|--------------------------------|
| Ring entries | `1..32768`, subject to the selected setup and package limits |
| SEND, RECV, SEND_ZC length | Fits a nonnegative signed 32-bit kernel length; a particular wrapper may require positive length |
| READV/WRITEV and fixed variants | `1..1024` iovecs; message operations permit `0..1024` |
| Read/write offset | Nonnegative signed 64-bit offset, or `-1` for current position where the API permits it |
| FileAdvise, Fallocate, SyncFileRange offset | Nonnegative; no current-position sentinel. Fallocate length is positive signed 64-bit |
| Timeout completion count | Unsigned 32-bit even though its SQE field is 64-bit |
| FUTEX_WAITV | `1..128` entries; operation flags zero |
| EPOLL_WAIT | Positive signed 32-bit event count; inline timeout stays zero |
| Socket domain/type/protocol and listen backlog | Nonnegative signed 32-bit values; operation-specific flags still apply |
| Shutdown | `SHUT_RD`, `SHUT_WR`, or `SHUT_RDWR` |
| Provided-buffer entry | Length `1..2^32-1`; ring size at most `2^15`; offset within the ring; publish count at most its size |
| Group/index/option | Check the specific table bound and consumer domain before casting; field width alone is insufficient |

Optional option fields use pointers when absence differs from zero. A signed sentinel is appropriate only when the owning API defines it. Preserve existing option fields and helpers. [Options](../../options_unix.go) owns defaults, registered-buffer counts, buffer tiers and group-span limits.

### Direct Files

An explicit direct slot `n` encodes as `n+1`. `IORING_FILE_INDEX_ALLOC` passes through unchanged; reject the adjacent value whose increment would collide with that sentinel. Direct multishot accept requires allocation. Direct sockets reject `SOCK_CLOEXEC`; fixed-file installation accepts only its documented installation flag. Distinguish an ordinary descriptor, registered slot and task-local registered-ring index.

`FILES_UPDATE` offsets are unsigned 32-bit, including the allocation sentinel. Retain its descriptor array through completion; allocation mode may write the selected indexes. This is separate from the synchronous registration update described below.

### Receive Shape

Multishot RECV uses buffer selection, zero address, zero message flags and a nonnegative signed 32-bit cap. Raw receive helpers require a nil buffer and select the FD/size-derived group; zero configured size selects the default group size. Managed `ReceiveMultishot` uses the context's group and zero cap.

Incremental receive is a one-shot selected-buffer RECV with positive signed 32-bit length, zero address and zero message flags. Its per-ID offsets belong to one receiver bound before the group's first receive. Bundles follow publication order. See [buffer ownership](resources.md#provided-buffers).

## Field Maps

These easily confused mappings retain their opcode-specific meanings. All other rejected/reserved fields must also be zero.

| Opcode | Required assignments | Required zeros |
|--------|----------------------|----------------|
| WAITID | `len=idtype`, `fd=id`, `file_index=options`, `addr2=infop` | `addr`, `buf_index`, `addr3`, `waitid_flags` |
| FUTEX_WAITV | `addr=vector`, `len=count` | `fd`, `buf_index`, `file_index`, `addr2`, `futex_flags`, `addr3` |
| FILES_UPDATE | `fd=-1`, `off=offset`, `addr=fds`, `len=count` | `rw_flags`, `splice_fd_in` |
| RECV_ZC | `fd=socket`, `ioprio=receive flags`, `len=length`, `zcrx_ifq_idx=queue` | `addr`, `addr2`, `addr3`, `msg_flags` |
| READ_MULTISHOT | `fd`, `off`, selected `buf_index`; set buffer selection | `addr`, `len` |

WAITID accepts `P_ALL`, `P_PID`, `P_PGID`, or `P_PIDFD`. IDs fit signed 32-bit: PID is positive; PGID/PIDFD are nonnegative; P_ALL ignores the ID. Options require at least one of `WEXITED`, `WSTOPPED`, `WCONTINUED` and only supported wait bits. RECV_ZC length/queue are unsigned 32-bit; receive flags require multishot, optionally poll-first.

## Setup and Capabilities

The default `MultiIssuers=true` permits a serialized Go owner to migrate between Linux tasks. Explicit single-issuer mode requires one locked OS thread from before `New` through `Stop`, including submission, enter/wait and resize. Never treat wrong-issuer `EEXIST` as success or would-block.

Probe opcode, setup and registration support separately; verify wrapper support too. Optional SQE128 changes the SQE stride. Context retention still copies only the 64-byte base SQE; Extended user bytes are not the SQE128 extension. Resize must preserve pending entries and their width. A remap failure after the kernel commits resize is terminal.

Version strings alone establish no usable capability. ZCRX control/query records and receive-page sizing depend on the actual kernel feature result and wrapper path.

## Registration

- Preserve syscall counts and nested query result/size fields. A successful query syscall does not imply every record succeeded.
- File-table updates may accept a prefix and clear the next slot before stopping. Mirror the kernel's changed slots, including that clear; do not mark the entire requested update successful. Descriptors below `-1` are invalid.
- Clone buffers from a known source registration and preserve the destination's local view. A registered-ring index names a task-local binding; retain that task/source correspondence.
- NAPI unregister uses `nr_args=1`; ZCRX control uses `nr_args=0`. Do not infer one registration's calling convention from another.
- Keep registered storage live through all kernel access and unregister/teardown. ZCRX refills must originate from delivered references; receive page size requires explicit feature support.

Sources: [operation encoding](../../operations_linux.go), [public wrappers](../../interface_linux.go), [ring setup and registration](../../io_uring_linux.go), [submission retention](../../submit_state.go), [kernel sources](../references.md). Validate applicable boundary cases and failure paths under [verification](../verify.md).
