// Mirror of dto.Environment in internal/adapter/http/dto. Only what screens
// read so far.
export type VerificationStatus = "pending" | "proof_ok" | "verified" | "verification_failed";

export type Environment = {
  id: string;
  name: string;
  base_url: string;
  verification_status: VerificationStatus;
  max_vus: number;
  max_duration_s: number;
  // Hours the target takes load, in its own timezone. Absent: any hour.
  allowed_window?: { start: string; end: string; timezone: string };
  created_at: string;
};
