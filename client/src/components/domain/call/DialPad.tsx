import { Button } from "@/components/ui/button";

const dialpadKeys: { digit: string; sub: string }[] = [
  { digit: "1", sub: "" },
  { digit: "2", sub: "ABC" },
  { digit: "3", sub: "DEF" },
  { digit: "4", sub: "GHI" },
  { digit: "5", sub: "JKL" },
  { digit: "6", sub: "MNO" },
  { digit: "7", sub: "PQRS" },
  { digit: "8", sub: "TUV" },
  { digit: "9", sub: "WXYZ" },
  { digit: "*", sub: "" },
  { digit: "0", sub: "+" },
  { digit: "#", sub: "" },
];

export const DialPad = ({ onKey }: { onKey: (char: string) => void }) => (
  <div className="grid grid-cols-3 gap-2">
    {dialpadKeys.map((key) => (
      <Button
        key={key.digit}
        type="button"
        variant="outline"
        className="h-14 flex-col gap-0.5"
        onClick={() => onKey(key.digit)}
      >
        <span className="font-mono text-lg font-medium leading-none">
          {key.digit}
        </span>
        <span className="text-[10px] leading-none text-muted-foreground">
          {key.sub || " "}
        </span>
      </Button>
    ))}
  </div>
);
