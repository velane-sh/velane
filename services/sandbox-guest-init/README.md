# Sandbox guest init

Guest-side protocol primitives for bounded quiesce and post-restore recovery. Quiesce drains work, revokes ephemeral credentials, `syncfs`es and freezes every writable mount, and produces a fresh nonce. Restore refuses workload readiness until the saved nonce is returned and fresh credentials rotate.
