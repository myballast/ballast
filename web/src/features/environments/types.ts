// Mirrors of the environment DTOs in internal/adapter/http/dto/environment.go.
export type VerificationStatus = "pending" | "proof_ok" | "verified" | "verification_failed";

export type Day = "mon" | "tue" | "wed" | "thu" | "fri" | "sat" | "sun";

// Days narrows the window to some days of the week, read in its timezone. Absent:
// every day. A window that crosses midnight belongs to the day it starts.
export type AllowedWindow = { start: string; end: string; timezone: string; days?: Day[] };

export type Environment = {
  id: string;
  name: string;
  base_url: string;
  verification_status: VerificationStatus;
  max_vus: number;
  max_duration_s: number;
  // Hours the target takes load, in its own timezone. Absent: any hour.
  allowed_window?: AllowedWindow;
  // When somebody last asked Ballast to fetch the challenge, whatever came of it.
  verification_checked_at?: string;
  created_at: string;
};

// What the target has to serve to prove somebody owns it. The token is meant to
// be published on the target, but only whoever manages environments reads it.
export type VerificationChallenge = {
  verification_status: VerificationStatus;
  token: string;
  url: string;
  // Present once an administrator has approved the target.
  verified_at?: string;
  verified_by?: string;
  // The approver as a screen says them: their name, or their address.
  verified_by_name?: string;
  checked_at?: string;
};

// What a failed verification says: where Ballast looked (with <token> where the
// token is), what answered, and how it differed. Never what the target's file
// said — only its status and its size.
export type ProofFailure = {
  searched: string;
  status?: number;
  bytes?: number;
  too_long?: boolean;
  reason: string;
};

// A stored credential, as the API describes it: a key and its dates. There is
// no value field, because no endpoint ever returns one.
export type Secret = {
  id: string;
  key: string;
  created_at: string;
  updated_at: string;
};
