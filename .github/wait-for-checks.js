const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const DEFAULT_MAX_ATTEMPTS = 120;
const DEFAULT_INTERVAL_MS = 10_000;
const MILLISECONDS_IN_SECOND = 1000;

module.exports = async function waitForChecks({
  github,
  context,
  core,
  checks,
}) {
  const owner = context.repo.owner;
  const repo = context.repo.repo;
  const ref = context.sha;
  // All required checks from CI and E2E workflows
  const defaultChecks = [
    // CI workflow jobs
    "go-tests",
    "lint",
    "web-tests",
    "mock-oauth-tests",
    // E2E workflow jobs
    "build-image",
    "build-mocks",
    "web-e2e",
    "cli-e2e",
  ];
  const desiredChecks =
    Array.isArray(checks) && checks.length > 0 ? checks : defaultChecks;
  const checkSet = new Set(desiredChecks);

  const maxAttempts = Number.parseInt(
    process.env.RACK_GATEWAY_WAIT_CHECKS_ATTEMPTS || String(DEFAULT_MAX_ATTEMPTS),
    10
  );
  const intervalMs = Number.parseInt(
    process.env.RACK_GATEWAY_WAIT_CHECKS_INTERVAL_MS || String(DEFAULT_INTERVAL_MS),
    10
  );

  core.info(
    `Waiting for checks [${desiredChecks.join(", ")}] on ${owner}/${repo}@${ref}`
  );

  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    // Paginate: scheduled workflows (e.g. the daily security scan) add check runs to the same commit,
    // which can push the required ones off the first page.
    const allRuns = await github.paginate(github.rest.checks.listForRef, {
      owner,
      repo,
      ref,
      per_page: 100,
    });

    // Only GitHub Actions runs count: another app (e.g. a code scanner) can post a check with the same name.
    // A re-run job adds another check run with the same name, and only the latest one counts.
    const latestByName = new Map();
    for (const run of allRuns) {
      if (!checkSet.has(run.name) || run.app?.slug !== "github-actions") {
        continue;
      }
      const latest = latestByName.get(run.name);
      if (!latest || run.id > latest.id) {
        latestByName.set(run.name, run);
      }
    }
    const runs = [...latestByName.values()];

    if (runs.length === checkSet.size) {
      const incomplete = runs.filter((run) => run.status !== "completed");
      if (incomplete.length === 0) {
        const failures = runs.filter((run) => run.conclusion !== "success");
        if (failures.length > 0) {
          const summary = failures
            .map((run) => `${run.name} -> ${run.conclusion}`)
            .join(", ");
          throw new Error(`Checks failed: ${summary}`);
        }

        core.info("All required checks completed successfully.");
        return;
      }
    }

    core.info(
      `Attempt ${attempt}/${maxAttempts}: checks not complete yet. Sleeping ${(
        intervalMs / MILLISECONDS_IN_SECOND
      ).toFixed(1)}s...`
    );
    await sleep(intervalMs);
  }

  throw new Error(
    `Timed out waiting for checks: ${desiredChecks.join(", ")}. Increase RACK_GATEWAY_WAIT_CHECKS_ATTEMPTS?`
  );
};
