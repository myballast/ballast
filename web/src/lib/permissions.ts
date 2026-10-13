// The permission names of the API, as GET /projects/{projectId}/permissions
// returns them. They mirror dto.PermissionName in internal/adapter/http/dto,
// which calls them part of the published contract.
//
// This file only ever hides what the caller cannot use. The API authorizes
// every request on its own, whatever the interface shows.
export type Permission =
  | "view_project"
  | "run_test"
  | "cancel_run"
  | "manage_scenarios"
  | "manage_environments"
  | "approve_target"
  | "manage_members"
  | "manage_project";

export type Access = {
  role: "viewer" | "runner" | "maintainer" | "admin";
  permissions: Permission[];
  custom: boolean;
};

export function can(access: Access, permission: Permission): boolean {
  return access.permissions.includes(permission);
}
