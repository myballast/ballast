// Mirrors of the environment DTOs in internal/adapter/http/dto/environment.go.
export type VerificationStatus = "pending" | "proof_ok" | "verified" | "verification_failed";

export type AllowedWindow = { start: string; end: string; timezone: string };

export type Environment = {
  id: string;
  name: string;
  base_url: string;
  verification_status: VerificationStatus;
  max_vus: number;
  max_duration_s: number;
  // Hours the target takes load, in its own timezone. Absent: any hour.
  allowed_window?: AllowedWindow;
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
};

// A stored credential, as the API describes it: a key and its dates. There is
// no value field, because no endpoint ever returns one.
export type Secret = {
  id: string;
  key: string;
  created_at: string;
  updated_at: string;
};
