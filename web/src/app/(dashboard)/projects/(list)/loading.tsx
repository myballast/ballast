import { SkeletonCards, SkeletonHeading, SkeletonPage } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading your projects…">
      <SkeletonHeading />
      <SkeletonCards />
    </SkeletonPage>
  );
}
