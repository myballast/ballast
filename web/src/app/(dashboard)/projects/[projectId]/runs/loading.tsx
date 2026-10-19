import { SkeletonHeading, SkeletonPage, SkeletonTable } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the runs…">
      <SkeletonHeading />
      <SkeletonTable rows={8} />
    </SkeletonPage>
  );
}
