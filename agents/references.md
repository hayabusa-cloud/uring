# References

This file guides web research and external-knowledge verification for code built above or beyond `code.hybscloud.com/uring`.

Use it when a claim depends on Linux ABI behavior, kernel implementation facts, memory-order facts, package-owner vocabulary, runtime observations, benchmark method, or formal-verification theory. Do not use external sources to widen the `code.hybscloud.com/uring` boundary: external facts must reduce back into the local boundary model in [`uring/agents/INDEX.md`](INDEX.md) and the formal guide files under [`uring/agents/`](./).

## Research Rule

Apply these steps to every external claim, in order; together they populate the `ResearchRecord` below and feed the Admission Test.

1. State the exact claim before searching, so the search cannot silently reshape it.
2. Classify the claim by kind: Linux ABI, syscall errno, memory ordering, `io_uring` feature, package-owner vocabulary, outcome semantics, resource lifecycle, benchmark method, environment fact, or proof method.
3. Search primary sources first; use secondary sources only to locate a primary source or to explain a conflict between primary sources.
4. Record the source URL, title, source class, version or kernel release when available, access date, and the exact fact used.
5. Reduce the claim into the local model: owner package, stratum, boundary fact, ownership epoch, outcome classification, capability condition, temporal condition, and policy owner.
6. When the claim affects code or guide semantics, record which local guide judgment or boundary rule it supports, and require enough local evidence to check the claim against that model before admission.
7. Mark any unclear or version-sensitive claim as `version_dependent`, `runtime_detected`, or `insufficient`, and never promote it to an unconditional rule.

## Primary Web Sources

Use these sources before blogs, forum posts, summaries, or examples.

- Linux kernel documentation: <https://docs.kernel.org/>
  Use for kernel-maintained documentation, subsystem notes, memory barriers, memory management, and feature documentation.

- Linux kernel user-space API guide: <https://docs.kernel.org/userspace-api/index.html>
  Use as the kernel documentation index for user-space-facing APIs. If a page is missing from the latest tree, check the versioned documentation matching the target kernel.

- Linux kernel source tree: <https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux.git/>
  Use for implementation facts. Pin a tag or commit when the claim is version-sensitive.

- Linux stable source tree: <https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/>
  Use when the target deployment is a stable or long-term maintenance branch rather than mainline.

- Linux next integration tree: <https://git.kernel.org/pub/scm/linux/kernel/git/next/linux-next.git/>
  Use only for pre-mainline integration evidence. Mark claims as provisional unless the change is merged into the target kernel tree.

- Linux `io_uring` source directory: <https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux.git/+/refs/heads/master/io_uring/>
  Use this as a browsing entry only. For any accepted implementation claim, replace `refs/heads/master` with the target kernel tag or commit before recording the fact.

- Linux `io_uring` UAPI header: <https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux.git/+/refs/heads/master/include/uapi/linux/io_uring.h>
  Use this as a browsing entry only. For any accepted UAPI claim, replace `refs/heads/master` with the target kernel tag or commit before recording the fact.

- Linux system-call table sources in the target architecture tree.
  Use the target kernel source for syscall numbering and ABI entry facts. Do not infer syscall numbers across architectures.

- Linux man-pages, `io_uring(7)`: <https://man7.org/linux/man-pages/man7/io_uring.7.html>
  Use for the userspace ABI overview and ring mechanics.

- Linux man-pages, `io_uring_setup(2)`: <https://man7.org/linux/man-pages/man2/io_uring_setup.2.html>
  Use for ring setup flags, setup return values, feature flags, mmap offsets, and setup-time capability facts.

- Linux man-pages, `io_uring_enter(2)`: <https://man7.org/linux/man-pages/man2/io_uring_enter.2.html>
  Use for submit, wait, syscall return, and enter-flag behavior.

- Linux man-pages, `io_uring_register(2)`: <https://man7.org/linux/man-pages/man2/io_uring_register.2.html>
  Use for registered buffers, fixed files, personalities, restrictions, and registered resources.

- Linux kernel memory barriers: <https://docs.kernel.org/core-api/wrappers/memory-barriers.html>
  Use for Linux-side ordering and device-memory claims. Treat this as a guide plus pointer to kernel memory-model material, not as a complete mathematical specification.

- Linux Kernel Memory Model documentation and tools: <https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/tools/memory-model>
  Use for formal memory-model claims. Pin the same kernel revision as the implementation claim.

- Linux networking documentation: <https://docs.kernel.org/networking/index.html>
  Use for socket, TCP, UDP, zero-copy, GRO/GSO, NIC, and packet-flow facts that affect `io_uring` network behavior.

- Linux `io_uring` zero-copy receive documentation: <https://docs.kernel.org/networking/iou-zcrx.html>
  Use for `IORING_OP_RECV_ZC`, ZC Rx setup, refill-ring, hardware, and capability claims.

- Linux block-layer documentation: <https://docs.kernel.org/block/index.html>
  Use for storage, queueing, direct I/O, and block-device behavior that affects `io_uring` storage claims.

- Linux filesystems documentation: <https://docs.kernel.org/filesystems/index.html>
  Use for filesystem-specific direct I/O, buffered I/O, writeback, and durability claims.

- Linux `mmap(2)`, `mlock(2)`, `madvise(2)`, `eventfd(2)`, `epoll(7)`, and `socket(7)` man-pages.
  Use for memory mapping, locked memory, advice, event notification, polling comparison, and socket behavior when the local code crosses those APIs.

- Kernel mailing-list archive: <https://lore.kernel.org/>
  Use for design rationale, patch discussion, regressions, and unresolved edge cases. Prefer messages from maintainers, patch authors, or subsystem reviewers, and tie every claim back to a commit, tag, or documented behavior when possible.

- `io-uring` mailing-list archive: <https://lore.kernel.org/io-uring/>
  Use for `io_uring`-specific patch discussion, regressions, and maintainer rationale. Do not treat discussion as final unless it is tied to merged code or documentation.

- Linux networking mailing-list archive: <https://lore.kernel.org/netdev/>
  Use for network-stack, socket, TCP, UDP, zero-copy, NIC, and driver behavior that affects `io_uring` networking paths.

- Linux block mailing-list archive: <https://lore.kernel.org/linux-block/>
  Use for block-layer, storage queueing, and direct-I/O behavior that affects `io_uring` storage paths.

- Linux filesystem mailing-list archive: <https://lore.kernel.org/linux-fsdevel/>
  Use for filesystem-specific I/O semantics, buffered/direct I/O, writeback, and file lifecycle behavior.

- Linux kernel patchwork: <https://patchwork.kernel.org/>
  Use to track patch state when a claim is about proposed or recently merged behavior. Do not treat unmerged patches as current behavior.

- Linux kernel releases: <https://www.kernel.org/>
  Use to confirm release status, stable status, and long-term maintenance status before claiming a target kernel supports a feature.

- Linux kernel release policy: <https://www.kernel.org/category/releases.html>
  Use to distinguish mainline, release candidate, stable, and long-term kernels before making version-support claims.

- Linux man-pages project source: <https://git.kernel.org/pub/scm/docs/man-pages/man-pages.git/>
  Use when the rendered man page and source need to be compared or when a man-page change is version-sensitive.

- Kernel bug tracker: <https://bugzilla.kernel.org/>
  Use for kernel bug reports and status. Reduce every claim to a kernel version, subsystem, reproduction, fix commit, or unresolved residual assumption.

- syzbot dashboard and syzkaller documentation: <https://syzkaller.appspot.com/> and <https://github.com/google/syzkaller/tree/master/docs>
  Use for fuzzing evidence and kernel crash reports. Treat a syzbot report as runtime evidence, not as an ABI rule.

- Go language specification: <https://go.dev/ref/spec>
  Use for Go syntax, typing, constants, unsafe surface, and language-level semantics.

- Go memory model: <https://go.dev/ref/mem>
  Use for Go happens-before, data-race, synchronization, and compiler-reordering claims.

- Go command documentation: <https://go.dev/doc/cmd>
  Use for `go test`, `go vet`, `gofmt`, `go tool`, and toolchain behavior.

- Go cgo command documentation: <https://go.dev/cmd/cgo/>
  Use only when a boundary claim depends on cgo, C ABI interaction, or pointer-passing constraints.

- Go issue tracker: <https://github.com/golang/go/issues>
  Use for proposal state, known toolchain bugs, and compatibility decisions. Do not treat issue discussion as specification unless it is tied to accepted documentation or released code.

- Go release notes: <https://go.dev/doc/devel/release>
  Use for version-specific language, runtime, compiler, linker, and toolchain changes.

- Go package documentation: <https://pkg.go.dev/>
  Use as an index for public GoDocs. For local packages, prefer the checked-out source and generated local docs over remote rendered docs.

- Go source tree: <https://go.googlesource.com/go>
  Use for toolchain implementation facts only after checking the trusted local Go version.

- Package-owner source for `code.hybscloud.com/uring`, `code.hybscloud.com/iox`, `code.hybscloud.com/iofd`, `code.hybscloud.com/iobuf`, `code.hybscloud.com/sock`, `code.hybscloud.com/framer`, `code.hybscloud.com/cove`, `code.hybscloud.com/kont`, `code.hybscloud.com/sess`, `code.hybscloud.com/takt`, `code.hybscloud.com/spin`, `code.hybscloud.com/lfq`, and `code.hybscloud.com/zcall`.
  Use the checked-out source tree and package GoDocs as the primary source for package-owned vocabulary. Do not infer package semantics from name similarity or downstream usage.

- Official package repository, release tag, and module metadata for each package dependency named above.
  Use when the checked-out source is not the target version. Pin the module version or commit before using the claim.

- POSIX.1 online specifications: <https://pubs.opengroup.org/onlinepubs/9799919799/>
  Use for portable Unix interface background only. Linux-specific behavior still requires Linux kernel or man-pages evidence.

- IETF RFC Editor: <https://www.rfc-editor.org/>
  Use for protocol specifications when code above or beyond `code.hybscloud.com/uring` implements TCP-adjacent, UDP-based, TLS-adjacent, HTTP, QUIC, DNS, or custom protocol behavior.

- IANA registries: <https://www.iana.org/protocols>
  Use for protocol numbers, port registries, media types, TLS registries, and other protocol constants.

- CVE records: <https://www.cve.org/>
  Use for vulnerability identifiers and descriptions.

- National Vulnerability Database: <https://nvd.nist.gov/>
  Use for vulnerability metadata, CVSS context, affected products, and references. Treat NVD enrichment as secondary to upstream fixes for exact code behavior.

- ACM Digital Library: <https://dl.acm.org/>
  Use for peer-reviewed programming-language, systems, concurrency, and verification papers.

- USENIX publications: <https://www.usenix.org/publications>
  Use for peer-reviewed systems papers, performance methodology, storage, networking, and OS work.

- IEEE Xplore: <https://ieeexplore.ieee.org/>
  Use for peer-reviewed systems and formal-methods papers when ACM or USENIX is not the publication venue.

- Springer LNCS: <https://link.springer.com/bookseries/558>
  Use for conference proceedings and formal-methods papers published through LNCS.

- arXiv: <https://arxiv.org/>
  Use as a preprint source. Prefer the proceedings version when it exists, and record the arXiv version when no proceedings version is available.

- Zenodo: <https://zenodo.org/>
  Use for archived artifacts, datasets, benchmark inputs, and DOI-backed supplementary material.

- Software Heritage: <https://www.softwareheritage.org/>
  Use to identify archived source snapshots when a proof artifact or benchmark artifact must be stable over time.

- Software Heritage archive: <https://archive.softwareheritage.org/>
  Use to resolve or create stable source identifiers for external artifacts, archived proof code, or benchmark code.

## Source Priority

For Linux ABI and `io_uring` behavior, use this order:

1. Target kernel source at a pinned tag or commit.
2. UAPI header at the same tag or commit.
3. Kernel documentation for the same or nearest available release.
4. Linux man-pages.
5. Kernel mailing-list discussion tied to a commit or patch series.
6. Patchwork, bug tracker, or syzbot evidence tied to a commit, reproducer, or kernel version.
7. Secondary summaries only as search aids.

For package-owner vocabulary, use this order:

1. Checked-out package source.
2. Package GoDocs generated from the checked-out source.
3. Public package documentation from the same module version.
4. External examples only as non-authoritative usage evidence.

For Go language and tool behavior, use this order:

1. Go specification, Go memory model, and Go command documentation.
2. Source under the trusted local Go toolchain when implementation behavior matters.
3. Proposal or issue discussion only when the primary docs are silent and the claim is marked provisional.

For formal methods and proof techniques, use this order:

1. Mechanized artifact or proof source.
2. Peer-reviewed paper or official proceedings version.
3. Author preprint matching the cited version.
4. Archived artifact, dataset, or benchmark input with a stable identifier.
5. Textbook or survey as background only.

For protocol and standards facts, use this order:

1. RFC, IANA registry, or POSIX specification.
2. Linux kernel or man-pages evidence for Linux-specific behavior.
3. Package-owner source for package-specific interpretation.
4. Secondary summaries only as search aids.

For security and bug facts, use this order:

1. Upstream fix commit or release note.
2. Kernel bug tracker, syzbot report, or package issue tied to a reproducer.
3. CVE record.
4. NVD enrichment.
5. Secondary writeups only as background.

## Admission Test

Admit a researched claim only when all of the following hold:

- The primary source is reachable or locally available.
- The source version, kernel release, or package revision is recorded when relevant.
- The exact fact used is quoted or summarized precisely.
- The claim reduces to a local boundary fact, carrier fact, below-kernel fact, caller frontier, or caller policy.
- Semantic claims identify the local guide judgment or boundary rule they support, with evidence that the claim can be checked against the local model.
- Outcome facts preserve successful completion, `ErrWouldBlock` from `code.hybscloud.com/iox`, `ErrMore` from `code.hybscloud.com/iox`, and failure as distinct cases.
- Package-owned facts keep their package owner.
- Caller policy remains outside the `code.hybscloud.com/uring` boundary and in the caller layer.

Reject or downgrade the claim when the source is secondary-only, version-ambiguous, contradicted by a primary source, or cannot be reduced into the local boundary model.

## Record Shape

Use this shape in research notes, lift records, or review artifacts:

```text
ResearchRecord =
  { claim
  , claim_kind
  , source_url
  , source_title
  , source_class
  , version_or_kernel_release
  , access_date
  , exact_fact_used
  , local_reduction
  , local_guide_mapping
  , owner_package
  , stratum
  , outcome_classification
  , capability_condition
  , temporal_condition
  , policy_owner
  , decision
  , residual_assumptions
  }

decision is one of: verified, version_dependent, runtime_detected, rejected, insufficient
```
