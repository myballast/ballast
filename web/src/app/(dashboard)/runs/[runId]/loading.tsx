import { SkeletonHeading, SkeletonPage, SkeletonPanel, SkeletonTiles } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the run…">
      <SkeletonHeading />
      <SkeletonTiles />
      <SkeletonPanel height="h-72" />
    </SkeletonPage>
  );
}
