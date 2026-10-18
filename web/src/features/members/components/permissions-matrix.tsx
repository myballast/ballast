import type { RolesMatrix } from "../types";

// PermissionsMatrix is the table a person reads before choosing a role: what
// each of the four may do. It is drawn from GET /roles, which derives it from
// the table the permission checks use, so what it shows is what is enforced.
//
// Allowed is a check mark and not allowed is a dot, each said in words for a
// screen reader: nothing here is a colour.
export function PermissionsMatrix({ matrix }: { matrix: RolesMatrix }) {
  return (
    <section className="relative overflow-x-auto rounded-xl border bg-card">
      <table className="w-full min-w-[40rem] border-collapse text-xs">
        <caption className="border-b px-4 py-3 text-left text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          Permissions
        </caption>
        <thead>
          <tr className="border-b text-muted-foreground">
            <th scope="col" className="px-4 py-2 text-left font-normal">
              Capability
            </th>
            {matrix.roles.map((role) => (
              <th key={role.role} scope="col" className="data w-24 px-2 py-2 text-center font-normal">
                {role.role}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {matrix.capabilities.map((capability) => (
            <tr key={capability.id} className="h-8 border-b last:border-b-0">
              <th scope="row" className="px-4 text-left font-normal text-foreground/90">
                {capability.label}
              </th>
              {matrix.roles.map((role) => {
                const allowed = role.capabilities.includes(capability.id);
                return (
                  <td key={role.role} className="px-2 text-center">
                    <span
                      role="img"
                      aria-label={allowed ? "allowed" : "not allowed"}
                      className={allowed ? "text-[11px]" : "text-[13px] text-muted-foreground"}
                    >
                      {allowed ? "✓" : "·"}
                    </span>
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
