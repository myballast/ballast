import type { Access } from "@/lib/permissions";

export type Role = Access["role"];

export type Member = {
  user_id: string;
  email: string;
  name?: string;
  role: Role;
  has_custom_permissions: boolean;
  created_at: string;
  // Null when the person has written nothing to the project's journal.
  last_activity: string | null;
};

// The permissions matrix, as GET /roles returns it: derived on the server from
// the table the checks use, so the table on screen is what is enforced.
export type Capability = { id: string; label: string };
export type RoleCapabilities = { role: Role; capabilities: string[] };
export type RolesMatrix = { capabilities: Capability[]; roles: RoleCapabilities[] };

export type Invitation = {
  id: string;
  email: string;
  role: Role;
  invited_by?: string;
  state: "pending" | "expired";
  expires_at: string;
  created_at: string;
};

export type InvitationState = "pending" | "accepted" | "revoked" | "expired";

export type InvitationPreview = {
  organization_name: string;
  project_name: string;
  email: string;
  role: Role;
  state: InvitationState;
  expires_at: string;
};
