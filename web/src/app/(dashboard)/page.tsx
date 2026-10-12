import { FolderKanban } from "lucide-react";

import { EmptyState } from "@/components/states";

// Where a sign-in lands when it was not started from a particular page. The
// project list that will live here comes with its own ticket (T26).
export default function HomePage() {
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-3xl font-normal tracking-tight">Projects</h1>
      <EmptyState
        icon={FolderKanban}
        title="Your projects will be listed here"
        description="Until then, open a run from its link to see its results."
      />
    </div>
  );
}
