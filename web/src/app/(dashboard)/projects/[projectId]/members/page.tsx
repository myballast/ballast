import { getInvitations, getMembers, getRoles } from "@/features/members/api";
import { MembersPage } from "@/features/members/components/members-page";
import { getAccess, getProject } from "@/features/projects/api";
import { ProjectHeader, RestrictedProject } from "@/features/projects/components/project-overview";
import { can } from "@/lib/permissions";

export default async function MembersRoute(props: PageProps<"/projects/[projectId]/members">) {
  const { projectId } = await props.params;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  if (!can(access, "view_project")) {
    return (
      <div className="flex flex-col gap-6">
        <ProjectHeader project={project} access={access} />
        <RestrictedProject />
      </div>
    );
  }

  // Only an admin may read the open invitations; asking anyone else would be
  // refused.
  const [members, matrix, invitations] = await Promise.all([
    getMembers(projectId),
    getRoles(),
    can(access, "manage_members") ? getInvitations(projectId) : Promise.resolve(null),
  ]);

  return (
    <MembersPage
      project={project}
      access={access}
      members={members}
      matrix={matrix}
      invitations={invitations}
      now={new Date()}
    />
  );
}
