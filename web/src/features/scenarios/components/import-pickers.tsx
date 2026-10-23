import { FileUp } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";

import { maxCurlBytes } from "../import-selection";

const dropzone =
  "flex cursor-pointer flex-col items-center gap-2 rounded-xl border border-dashed px-6 py-12 text-center text-sm has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring";

function Refusal({ error }: { error: string }) {
  if (!error) return null;
  return (
    <p role="alert" className="rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
      {error}
    </p>
  );
}

// FileDropzone is the box that takes one file and starts the preview.
function FileDropzone({
  pending,
  title,
  hint,
  accept,
  onFile,
}: {
  pending: boolean;
  title: string;
  hint: string;
  accept: string;
  onFile: (file: File) => void;
}) {
  return (
    <label className={dropzone}>
      <FileUp aria-hidden className="size-6 text-muted-foreground" />
      <span className="font-medium">{pending ? "Reading the file…" : title}</span>
      <span className="text-xs text-muted-foreground">{hint}</span>
      <input
        type="file"
        accept={accept}
        disabled={pending}
        className="sr-only"
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (file) onFile(file);
          // Choosing the same file again has to fire a change.
          event.target.value = "";
        }}
      />
    </label>
  );
}

// CollectionPicker is the first step for Postman: the environment file, if there
// is one, and then the collection, which starts the preview.
export function CollectionPicker({
  pending,
  error,
  environmentName,
  onCollection,
  onEnvironment,
}: {
  pending: boolean;
  error: string;
  environmentName: string;
  onCollection: (file: File) => void;
  onEnvironment: (file: File) => void;
}) {
  return (
    <div className="flex max-w-xl flex-col gap-4">
      <label className="flex flex-col gap-1.5 text-sm">
        <span>
          Environment file <span className="text-muted-foreground">(optional)</span>
        </span>
        <input
          type="file"
          accept=".json,application/json"
          disabled={pending}
          className="text-xs"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) onEnvironment(file);
          }}
        />
        <span className="text-xs text-muted-foreground">
          {environmentName
            ? `${environmentName} will be read with the collection.`
            : "Choose it before the collection: it says which variables are secrets, and gives their values to the rest."}
        </span>
      </label>

      <FileDropzone
        pending={pending}
        title="Choose a Postman collection"
        hint="Collection v2.0 or v2.1, JSON, up to 5 MB"
        accept=".json,application/json"
        onFile={onCollection}
      />
      <Refusal error={error} />
    </div>
  );
}

// HARPicker takes the recording a browser's network tab exports.
export function HARPicker({
  pending,
  error,
  onFile,
}: {
  pending: boolean;
  error: string;
  onFile: (file: File) => void;
}) {
  return (
    <div className="flex max-w-xl flex-col gap-4">
      <FileDropzone
        pending={pending}
        title="Choose a HAR file"
        hint="Exported from the network tab of a browser, up to 25 MB"
        accept=".har,.json,application/json"
        onFile={onFile}
      />
      <Refusal error={error} />
    </div>
  );
}

// CurlPicker takes pasted commands, as copied from a browser or a terminal.
export function CurlPicker({
  pending,
  error,
  onCommands,
}: {
  pending: boolean;
  error: string;
  onCommands: (text: string) => void;
}) {
  const [text, setText] = useState("");
  const tooLong = new Blob([text]).size > maxCurlBytes;

  return (
    <form
      className="flex max-w-3xl flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        onCommands(text);
      }}
    >
      <label className="flex flex-col gap-1.5 text-sm">
        cURL commands
        <textarea
          value={text}
          onChange={(event) => setText(event.target.value)}
          rows={10}
          spellCheck={false}
          placeholder={"curl 'https://api.example.com/orders' -H 'accept: application/json'"}
          className="data rounded-md border bg-background p-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <span className="text-xs text-muted-foreground">
          One or several commands, as copied from a browser (Copy as cURL) or a terminal. Nothing is run: the text is only
          read. Up to 256 KB.
        </span>
      </label>

      {tooLong && (
        <p role="alert" className="text-sm text-muted-foreground">
          This text is longer than 256 KB, the most Ballast reads.
        </p>
      )}
      <Refusal error={error} />

      <div>
        <Button type="submit" disabled={pending || text.trim() === "" || tooLong}>
          {pending ? "Reading…" : "Read the commands"}
        </Button>
      </div>
    </form>
  );
}
