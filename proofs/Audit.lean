-- ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
-- Use of this source code is governed by the MIT license in ../LICENSE.

import ZeroCopy
import Cancellation
import Incremental
import CompletionQueue
import Context

#print axioms Uring.ZeroCopy.at_most_once
#print axioms Uring.ZeroCopy.release_requires_completion
#print axioms Uring.ZeroCopy.release_requires_evidence
#print axioms Uring.ZeroCopy.terminal_absorbing
#print axioms Uring.ZeroCopy.accepted_result_stable
#print axioms Uring.ZeroCopy.result_stable_through_trace
#print axioms Uring.ZeroCopy.notification_orders
#print axioms Uring.ZeroCopy.terminal_fallback

#print axioms Uring.Cancellation.at_most_once
#print axioms Uring.Cancellation.retirement_requires_observation
#print axioms Uring.Cancellation.publisher_keeps_context
#print axioms Uring.Cancellation.failed_admission_has_no_credit
#print axioms Uring.Cancellation.completion_orders

#print axioms Uring.Incremental.offsets_bounded
#print axioms Uring.Incremental.other_ids_unchanged
#print axioms Uring.Incremental.fragment_bounds
#print axioms Uring.Incremental.selected_offset
#print axioms Uring.Incremental.retained_adjacency
#print axioms Uring.Incremental.nonpositive_return
#print axioms Uring.Incremental.nonpositive_retained

#print axioms Uring.CompletionQueue.copied_prefix
#print axioms Uring.CompletionQueue.no_duplicate_copies
#print axioms Uring.CompletionQueue.publication_follows_copy
#print axioms Uring.CompletionQueue.copies_are_published_by_kernel
#print axioms Uring.CompletionQueue.distance_across_wrap
#print axioms Uring.CompletionQueue.claim_sequence_unique

#print axioms Uring.Context.direct_roundtrip
#print axioms Uring.Context.direct_injective
#print axioms Uring.Context.flags_replacement
#print axioms Uring.Context.signed_fd_roundtrip
#print axioms Uring.Context.pointer_roundtrip
#print axioms Uring.Context.modes_disjoint
