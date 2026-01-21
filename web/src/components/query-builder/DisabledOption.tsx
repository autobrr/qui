import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

interface DisabledOptionProps {
  children: ReactNode;
  reason: string;
  className?: string;
}

export function DisabledOption({ children, reason, className }: DisabledOptionProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div
          className={cn(
            "relative flex cursor-not-allowed select-none items-center rounded-sm text-sm opacity-50 outline-none",
            className
          )}
          role="option"
          aria-disabled="true"
        >
          {children}
        </div>
      </TooltipTrigger>
      <TooltipContent side="right" className="max-w-[200px]">
        {reason}
      </TooltipContent>
    </Tooltip>
  );
}
