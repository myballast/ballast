import { SkeletonHeading, SkeletonPage, SkeletonPanel, SkeletonTable } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the members…">
      <SkeletonHeading />
      <SkeletonPanel height="h-52" />
      <SkeletonTable rows={5} />
    </SkeletonPage>
  );
}
