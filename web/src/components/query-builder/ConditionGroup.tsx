/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { ConditionOperator, RuleCondition } from "@/types";
import {
  SortableContext,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { Plus, X } from "lucide-react";
import { useCallback } from "react";
import type { DisabledField, DisabledStateValue } from "./constants";
import { LeafCondition } from "./LeafCondition";

interface ConditionGroupProps {
  id: string;
  condition: RuleCondition;
  onChange: (condition: RuleCondition) => void;
  onRemove?: () => void;
  depth?: number;
  isRoot?: boolean;
  /** Optional category options for EXISTS_IN/CONTAINS_IN operators */
  categoryOptions?: Array<{ label: string; value: string }>;
  /** Optional list of fields to disable with reasons */
  disabledFields?: DisabledField[];
  /** Optional list of "state" option values to disable with reasons */
  disabledStateValues?: DisabledStateValue[];
}

const MAX_DEPTH = 5;

export function ConditionGroup({
  id,
  condition,
  onChange,
  onRemove,
  depth = 0,
  isRoot = false,
  categoryOptions,
  disabledFields,
  disabledStateValues,
}: ConditionGroupProps) {
  const isGroup = condition.operator === "AND" || condition.operator === "OR";
  const children = condition.conditions ?? [];

  const toggleOperator = useCallback(() => {
    onChange({
      ...condition,
      operator: (condition.operator === "AND" ? "OR" : "AND") as ConditionOperator,
    });
  }, [condition, onChange]);

  const addCondition = useCallback(() => {
    const newCondition: RuleCondition = {
      clientId: `c_${Math.random().toString(36).slice(2, 10)}_${Date.now().toString(36)}`,
      field: "NAME",
      operator: "CONTAINS",
      value: "",
    };
    onChange({
      ...condition,
      conditions: [...children, newCondition],
    });
  }, [condition, children, onChange]);

  const addGroup = useCallback(() => {
    if (depth >= MAX_DEPTH) return;

    const newGroup: RuleCondition = {
      clientId: `c_${Math.random().toString(36).slice(2, 10)}_${Date.now().toString(36)}`,
      operator: "AND",
      conditions: [
        {
          clientId: `c_${Math.random().toString(36).slice(2, 10)}_${Date.now().toString(36)}`,
          field: "NAME",
          operator: "CONTAINS",
          value: "",
        },
      ],
    };
    onChange({
      ...condition,
      conditions: [...children, newGroup],
    });
  }, [condition, children, depth, onChange]);

  const updateChild = useCallback(
    (index: number, updated: RuleCondition) => {
      const newChildren = [...children];
      newChildren[index] = updated;
      onChange({
        ...condition,
        conditions: newChildren,
      });
    },
    [condition, children, onChange]
  );

  const removeChild = useCallback(
    (index: number) => {
      const newChildren = children.filter((_, i) => i !== index);
      // If removing leaves only one child in a non-root group, replace group with child
      if (!isRoot && newChildren.length === 1) {
        onChange(newChildren[0]);
      } else if (newChildren.length === 0) {
        // Remove empty group (or clear root when allowEmpty)
        if (onRemove) {
          onRemove();
        } else {
          // Root without onRemove: update with empty children (handleChange normalizes to null)
          onChange({ ...condition, conditions: newChildren });
        }
      } else {
        onChange({
          ...condition,
          conditions: newChildren,
        });
      }
    },
    [condition, children, isRoot, onChange, onRemove]
  );

  // For leaf conditions, render LeafCondition
  if (!isGroup) {
    return (
      <LeafCondition
        id={id}
        condition={condition}
        onChange={onChange}
        onRemove={onRemove ?? (() => {})}
        categoryOptions={categoryOptions}
        disabledFields={disabledFields}
        disabledStateValues={disabledStateValues}
      />
    );
  }

  // Generate unique IDs for children
  const childIds = children.map((child, index) => child.clientId ?? `${id}-${index}`);

  return (
    <div
      className={cn(
        "rounded-lg border p-2 sm:p-3",
        depth === 0 && "border-border bg-card",
        depth > 0 && "border-border/50 bg-muted/30",
        depth > 1 && "border-dashed"
      )}
    >
      <div className="mb-2 flex items-center gap-2">
        {/* Operator toggle */}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className={cn(
            "h-7 px-3 font-mono text-xs font-semibold",
            condition.operator === "AND"
              ? "border-blue-500/50 bg-blue-500/10 text-blue-500"
              : "border-orange-500/50 bg-orange-500/10 text-orange-500"
          )}
          onClick={toggleOperator}
        >
          {condition.operator}
        </Button>
        <span className="text-xs text-muted-foreground">
          {condition.operator === "AND" ? "All conditions must match" : "Any condition must match"}
        </span>

        {/* Remove group button (not for root) */}
        {!isRoot && onRemove && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="ml-auto h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
            onClick={onRemove}
          >
            <X className="size-4" />
          </Button>
        )}
      </div>

      {/* Children */}
      <SortableContext items={childIds} strategy={verticalListSortingStrategy}>
        <div className="space-y-2">
          {children.map((child, index) => {
            const childId = childIds[index];
            const isChildGroup = child.operator === "AND" || child.operator === "OR";

            if (isChildGroup) {
              return (
                <ConditionGroup
                  key={childId}
                  id={childId}
                  condition={child}
                  onChange={(updated) => updateChild(index, updated)}
                  onRemove={() => removeChild(index)}
                  depth={depth + 1}
                  categoryOptions={categoryOptions}
                  disabledFields={disabledFields}
                  disabledStateValues={disabledStateValues}
                />
              );
            }

            return (
              <LeafCondition
                key={childId}
                id={childId}
                condition={child}
                onChange={(updated) => updateChild(index, updated)}
                onRemove={() => removeChild(index)}
                isOnly={children.length === 1 && isRoot && !onRemove}
                categoryOptions={categoryOptions}
                disabledFields={disabledFields}
                disabledStateValues={disabledStateValues}
              />
            );
          })}
        </div>
      </SortableContext>

      {/* Add buttons */}
      <div className="mt-2 flex gap-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 text-xs"
          onClick={addCondition}
        >
          <Plus className="mr-1 size-3" />
          Condition
        </Button>
        {depth < MAX_DEPTH && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 text-xs"
            onClick={addGroup}
          >
            <Plus className="mr-1 size-3" />
            Group
          </Button>
        )}
      </div>
    </div>
  );
}
