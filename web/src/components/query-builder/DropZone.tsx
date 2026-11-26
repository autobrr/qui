/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils";
import { useDroppable } from "@dnd-kit/core";

interface DropZoneProps {
  id: string
}

export function DropZone({ id }: DropZoneProps) {
  const { isOver, setNodeRef } = useDroppable({ id });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "h-2 rounded-sm border border-transparent transition-colors",
        isOver && "border-primary/60 bg-primary/20"
      )}
      aria-hidden="true"
    />
  );
}
