import { SkeletonHeading, SkeletonPage, SkeletonPanel } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the scenario…">
      <SkeletonHeading />
      <SkeletonPanel height="h-80" />
      <SkeletonPanel height="h-32" />
    </SkeletonPage>
  );
}
