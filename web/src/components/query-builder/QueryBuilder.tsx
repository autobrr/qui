/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button";
import type { RuleCondition } from "@/types";
import type { DragEndEvent } from "@dnd-kit/core";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  sortableKeyboardCoordinates,
} from "@dnd-kit/sortable";
import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ConditionGroup, parseDropZoneID } from "./ConditionGroup";
import type { DisabledField, DisabledStateValue } from "./constants";

export interface GroupOption {
  id: string
  label: string
  description?: string
}

interface QueryBuilderProps {
  condition: RuleCondition | null;
  onChange: (condition: RuleCondition | null) => void;
  className?: string;
  /** Allow a truly empty condition (null) state instead of auto-inserting a placeholder rule. */
  allowEmpty?: boolean;
  /** Optional category options for EXISTS_IN/CONTAINS_IN operators */
  categoryOptions?: Array<{ label: string; value: string }>;
  /** Optional list of fields to disable with reasons */
  disabledFields?: DisabledField[];
  /** Optional list of "state" option values to disable with reasons */
  disabledStateValues?: DisabledStateValue[];
  /** Available grouping IDs for GROUP_SIZE / IS_GROUPED leaf conditions */
  groupOptions?: GroupOption[];
}

export function QueryBuilder({
  condition,
  onChange,
  className,
  allowEmpty,
  categoryOptions,
  disabledFields,
  disabledStateValues,
  groupOptions,
}: QueryBuilderProps) {
  const { t } = useTranslation("common");
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never));
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8,
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

  // Initialize with a default AND group if empty
  const effectiveCondition = useMemo<RuleCondition | null>(() => {
    if (!condition) {
      if (allowEmpty) {
        return null;
      }
      return ensureClientIdsDeep({
        clientId: generateClientId(),
        operator: "AND",
        conditions: [
          {
            clientId: generateClientId(),
            field: "NAME",
            operator: "CONTAINS",
            value: "",
          },
        ],
      });
    }
    // Wrap non-group conditions in a group
    if (condition.operator !== "AND" && condition.operator !== "OR") {
      return ensureClientIdsDeep({
        clientId: generateClientId(),
        operator: "AND",
        conditions: [ensureClientIdsDeep(condition)],
      });
    }
    return ensureClientIdsDeep(condition);
  }, [allowEmpty, condition]);

  const handleChange = useCallback(
    (updated: RuleCondition) => {
      // If the root condition has no children, set to null
      if (
        (updated.operator === "AND" || updated.operator === "OR") &&
        (!updated.conditions || updated.conditions.length === 0)
      ) {
        onChange(null);
        return;
      }
      onChange(updated);
    },
    [onChange]
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      if (!effectiveCondition) return;
      const { active, over } = event;
      if (!over || active.id === over.id) return;

      const activeIdStr = active.id as string;
      const overIdStr = over.id as string;

      const activePath = findPathByClientId(effectiveCondition, activeIdStr);
      if (!activePath) return;

      const dropZone = parseDropZoneID(overIdStr);
      if (dropZone) {
        const movedCondition = moveNodeToGroupIndex(
          effectiveCondition,
          activePath,
          dropZone.groupID,
          dropZone.index
        );
        if (movedCondition) {
          handleChange(movedCondition);
        }
        return;
      }

      if (overIdStr === effectiveCondition.clientId) {
        const movedCondition = moveNodeToGroupIndex(
          effectiveCondition,
          activePath,
          effectiveCondition.clientId ?? "root",
          (effectiveCondition.conditions ?? []).length
        );
        if (movedCondition) {
          handleChange(movedCondition);
        }
        return;
      }

      const overPath = findPathByClientId(effectiveCondition, overIdStr);
      if (!overPath) return;

      // Prevent moving a group into one of its own descendants.
      if (isAncestorPath(activePath, overPath)) return;

      const overNode = getNodeAtPath(effectiveCondition, overPath);
      if (overNode && isGroupCondition(overNode)) {
        const movedCondition = moveNodeToGroupIndex(
          effectiveCondition,
          activePath,
          overIdStr,
          (overNode.conditions ?? []).length
        );
        if (movedCondition) {
          handleChange(movedCondition);
        }
        return;
      }

      if (!pathsHaveSameParent(activePath, overPath)) {
        const movedCondition = moveNodeToPathIndex(
          effectiveCondition,
          activePath,
          overPath.slice(0, -1),
          overPath[overPath.length - 1]
        );
        if (movedCondition) {
          handleChange(movedCondition);
        }
        return;
      }

      const parentPath = activePath.slice(0, -1);
      const activeIndex = activePath[activePath.length - 1];
      const overIndex = overPath[overPath.length - 1];

      // Get the parent group and reorder its children
      const newCondition = reorderAtPath(
        effectiveCondition,
        parentPath,
        activeIndex,
        overIndex
      );

      if (newCondition) {
        handleChange(newCondition);
      }
    },
    [effectiveCondition, handleChange]
  );

  const addFirstCondition = useCallback(() => {
    onChange(ensureClientIdsDeep({
      clientId: generateClientId(),
      operator: "AND",
      conditions: [
        {
          clientId: generateClientId(),
          field: "NAME",
          operator: "CONTAINS",
          value: "",
        },
      ],
    }));
  }, [onChange]);

  if (allowEmpty && !effectiveCondition) {
    return (
      <div className={className}>
        <div className="rounded-lg border border-dashed bg-muted/30 px-3 py-4 flex items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-sm font-medium">{tr("queryBuilder.emptyState.title")}</p>
            <p className="text-xs text-muted-foreground">
              {tr("queryBuilder.emptyState.description")}
            </p>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={addFirstCondition}>
            {tr("queryBuilder.emptyState.action")}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragEnd={handleDragEnd}
    >
      <div className={className}>
        <ConditionGroup
          id={effectiveCondition!.clientId ?? "root"}
          condition={effectiveCondition!}
          onChange={handleChange}
          onRemove={allowEmpty ? () => onChange(null) : undefined}
          isRoot
          categoryOptions={categoryOptions}
          disabledFields={disabledFields}
          disabledStateValues={disabledStateValues}
          groupOptions={groupOptions}
        />
      </div>
    </DndContext>
  );
}

function generateClientId(): string {
  const cryptoObj = globalThis.crypto as Crypto | undefined;
  if (cryptoObj?.randomUUID) {
    return cryptoObj.randomUUID();
  }
  return `c_${Math.random().toString(36).slice(2, 10)}_${Date.now().toString(36)}`;
}

function ensureClientIdsDeep(condition: RuleCondition): RuleCondition {
  const desiredClientId = condition.clientId ?? generateClientId();

  let nextChildren: RuleCondition[] | undefined = condition.conditions;
  if (condition.conditions) {
    let changed = false;
    const mapped = condition.conditions.map((child) => {
      const ensured = ensureClientIdsDeep(child);
      if (ensured !== child) {
        changed = true;
      }
      return ensured;
    });
    if (changed) {
      nextChildren = mapped;
    }
  }

  if (desiredClientId === condition.clientId && nextChildren === condition.conditions) {
    return condition;
  }

  return {
    ...condition,
    clientId: desiredClientId,
    conditions: nextChildren,
  };
}

// Helper: Find a node by clientId and return its index path.
function findPathByClientId(root: RuleCondition, clientId: string): number[] | null {
  if (!root.conditions) return null;

  for (let i = 0; i < root.conditions.length; i++) {
    const child = root.conditions[i];
    if (child.clientId === clientId) {
      return [i];
    }
    const sub = findPathByClientId(child, clientId);
    if (sub) {
      return [i, ...sub];
    }
  }

  return null;
}

// Helper: Check if two paths have the same parent
function pathsHaveSameParent(path1: number[], path2: number[]): boolean {
  if (path1.length !== path2.length) return false;
  const parent1 = path1.slice(0, -1);
  const parent2 = path2.slice(0, -1);
  return parent1.length === parent2.length && parent1.every((v, i) => v === parent2[i]);
}

// Helper: Reorder children at a given path
function reorderAtPath(
  root: RuleCondition,
  parentPath: number[],
  fromIndex: number,
  toIndex: number
): RuleCondition | null {
  if (!root.conditions) return null;

  // Reorder at root level
  if (parentPath.length === 0) {
    return { ...root, conditions: arrayMove(root.conditions, fromIndex, toIndex) };
  }

  const newRoot: RuleCondition = { ...root, conditions: [...root.conditions] };
  let current: RuleCondition = newRoot;

  for (const index of parentPath) {
    if (!current.conditions?.[index]) return null;
    const next = current.conditions[index];
    const clonedNext: RuleCondition = {
      ...next,
      conditions: next.conditions ? [...next.conditions] : next.conditions,
    };
    current.conditions[index] = clonedNext;
    current = clonedNext;
  }

  if (!current.conditions) return null;
  current.conditions = arrayMove(current.conditions, fromIndex, toIndex);
  return newRoot;
}

function isAncestorPath(path: number[], candidateDescendant: number[]): boolean {
  if (path.length >= candidateDescendant.length) return false;
  return path.every((segment, index) => segment === candidateDescendant[index]);
}

function isSamePath(pathA: number[], pathB: number[]): boolean {
  if (pathA.length !== pathB.length) return false;
  return pathA.every((segment, index) => segment === pathB[index]);
}

function cloneConditionTree(condition: RuleCondition): RuleCondition {
  return {
    ...condition,
    conditions: condition.conditions?.map(cloneConditionTree),
  };
}

function getNodeAtPath(root: RuleCondition, path: number[]): RuleCondition | null {
  let current: RuleCondition = root;
  for (const index of path) {
    const next = current.conditions?.[index];
    if (!next) {
      return null;
    }
    current = next;
  }
  return current;
}

function resolveGroupPath(root: RuleCondition, groupID: string): number[] | null {
  if (root.clientId === groupID) {
    return [];
  }
  return findPathByClientId(root, groupID);
}

function isGroupCondition(condition: RuleCondition | null | undefined): boolean {
  if (!condition) return false;
  return condition.operator === "AND" || condition.operator === "OR";
}

function moveNodeToGroupIndex(
  root: RuleCondition,
  sourcePath: number[],
  targetGroupID: string,
  targetIndex: number
): RuleCondition | null {
  const targetGroupPath = resolveGroupPath(root, targetGroupID);
  if (targetGroupPath == null) {
    return null;
  }
  return moveNodeToPathIndex(root, sourcePath, targetGroupPath, targetIndex);
}

function moveNodeToPathIndex(
  root: RuleCondition,
  sourcePath: number[],
  targetParentPath: number[],
  targetIndex: number
): RuleCondition | null {
  if (sourcePath.length === 0) return null;
  if (targetIndex < 0) return null;

  const sourceNode = getNodeAtPath(root, sourcePath);
  if (isGroupCondition(sourceNode) &&
    (isAncestorPath(sourcePath, targetParentPath) || isSamePath(sourcePath, targetParentPath))) {
    return null;
  }

  const nextRoot = cloneConditionTree(root);
  const sourceParentPath = sourcePath.slice(0, -1);
  const sourceIndex = sourcePath[sourcePath.length - 1];
  const sourceParent = getNodeAtPath(nextRoot, sourceParentPath);
  if (!sourceParent?.conditions?.[sourceIndex]) return null;

  const [moved] = sourceParent.conditions.splice(sourceIndex, 1);
  if (!moved) return null;
  const adjustedTargetParentPath = adjustPathAfterRemoval(targetParentPath, sourcePath);
  if (!adjustedTargetParentPath) return null;
  const targetParent = getNodeAtPath(nextRoot, adjustedTargetParentPath);
  if (!targetParent?.conditions || !isGroupCondition(targetParent)) return null;

  let insertIndex = targetIndex;
  if (isSamePath(sourceParentPath, adjustedTargetParentPath) && sourceIndex < targetIndex) {
    insertIndex -= 1;
  }
  insertIndex = Math.max(0, Math.min(insertIndex, targetParent.conditions.length));

  targetParent.conditions.splice(insertIndex, 0, moved);
  return pruneEmptyGroups(nextRoot, true);
}

function adjustPathAfterRemoval(path: number[], removedPath: number[]): number[] | null {
  if (path.length === 0 || removedPath.length === 0) {
    return path;
  }

  const removedParentPath = removedPath.slice(0, -1);
  const removedIndex = removedPath[removedPath.length - 1];

  // If parent path diverges before the removal parent, no adjustment is needed.
  for (let i = 0; i < removedParentPath.length; i++) {
    if (i >= path.length || path[i] !== removedParentPath[i]) {
      return path;
    }
  }

  // Same parent path (or root move) => parent container remains valid.
  if (path.length === removedParentPath.length) {
    return path;
  }

  const divergingSegment = removedParentPath.length;
  const currentIndex = path[divergingSegment];

  // Target path points into removed node subtree; treat as invalid.
  if (currentIndex === removedIndex) {
    return null;
  }

  if (currentIndex > removedIndex) {
    const adjusted = [...path];
    adjusted[divergingSegment] = currentIndex - 1;
    return adjusted;
  }

  return path;
}

function pruneEmptyGroups(condition: RuleCondition, isRoot: boolean): RuleCondition | null {
  if (condition.operator !== "AND" && condition.operator !== "OR") {
    return condition;
  }

  const cleanedChildren = (condition.conditions ?? [])
    .map((child) => pruneEmptyGroups(child, false))
    .filter((child): child is RuleCondition => child !== null);

  if (!isRoot && cleanedChildren.length === 0) {
    return null;
  }

  return {
    ...condition,
    conditions: cleanedChildren,
  };
}
