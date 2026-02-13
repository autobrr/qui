/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { ReactElement, ReactNode } from "react";
import { cloneElement, isValidElement } from "react";

type DisabledOptionChildProps = {
  className?: string;
  disabled?: boolean;
  "aria-disabled"?: boolean;
};

interface DisabledOptionProps {
  children: ReactNode;
  reason: string;
  className?: string;
}

export function DisabledOption({ children, reason, className }: DisabledOptionProps) {
  let child: ReactElement;

  if (isValidElement<DisabledOptionChildProps>(children)) {
    child = cloneElement(children, {
      className: cn(
        children.props.className,
        "cursor-not-allowed data-[disabled]:pointer-events-auto data-[disabled=true]:pointer-events-auto",
        className
      ),
      disabled: true,
      "aria-disabled": true,
    });
  } else {
    child = (
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
    );
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
         <div>
           {child}
         </div>
      </TooltipTrigger>
      <TooltipContent side="right" className="max-w-[200px]">
        {reason}
      </TooltipContent>
    </Tooltip>
  );
}
