import { SkeletonHeading, SkeletonPage, SkeletonPanel } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the environment…">
      <SkeletonHeading />
      <SkeletonPanel height="h-64" />
      <SkeletonPanel height="h-40" />
    </SkeletonPage>
  );
}
