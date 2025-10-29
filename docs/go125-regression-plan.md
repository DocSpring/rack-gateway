# Go 1.25 MFA Regression Debug Plan

1. **Toolchain isolation**
   - Revert only the Go toolchain bump (keep dependency upgrades).
   - Re-run the Account Security shard to see if the regression disappears.
2. **Dependency bisection**
   - If failure persists, restore toolchain update and revert half of the upgraded modules.
   - Re-test shard; continue halving until the offending package is found.
3. **Unit test coverage**
   - Once the culprit is identified, add targeted Go unit tests around MFA step-up/session logic.
4. **Fix & verification**
   - Implement fix or pin dependency.
   - Re-run shard, `task web:e2e`, `task go:test`, and `bun run test`.

## Progress Notes (2025-10-29)
- ✅ Reverted only the toolchain bump (Go 1.24) — full suite still fails ➜ regression not isolated to toolchain.
- ✅ Restored Go 1.25 while reverting dependency upgrades to bd0aeb4 → `task web:e2e` passes. Culprit lives in dependency upgrades.
- ➡️ Next: begin bisection on the upgraded Go modules (Step 2).
