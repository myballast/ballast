import { SkeletonHeading, SkeletonPage, SkeletonTable } from "@/components/skeleton";

export default function Loading() {
  return (
    <SkeletonPage label="Loading the webhook…">
      <SkeletonHeading />
      <SkeletonTable rows={2} />
    </SkeletonPage>
  );
}
