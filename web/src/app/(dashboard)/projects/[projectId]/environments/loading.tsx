import { SkeletonHeading, SkeletonPage, SkeletonTable } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the environments…">
      <SkeletonHeading />
      <SkeletonTable rows={5} />
    </SkeletonPage>
  );
}
