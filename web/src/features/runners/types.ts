// Mirrors of the runner DTOs in internal/adapter/http/dto/runner.go.

// Read against the clock by the API when the list is asked for, never stored.
export type RunnerStatus = "online" | "draining" | "offline";

export type Runner = {
  name: string;
  // The build, for a person to read.
  version: string;
  // What the runner and the database promise each other; compared, not displayed
  // as a version.
  contract: number;
  status: RunnerStatus;
  // Below the minimum contract: the runner is up, and takes no run.
  outdated: boolean;
  started_at: string;
  last_seen_at: string;
};

export type RunnerList = {
  runners: Runner[];
  minimum_contract: number;
};
