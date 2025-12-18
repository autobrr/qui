import { useCallback, useMemo } from "react";
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
import type { RuleCondition } from "@/types";
import { ConditionGroup } from "./ConditionGroup";

interface QueryBuilderProps {
  condition: RuleCondition | null;
  onChange: (condition: RuleCondition | null) => void;
  className?: string;
}

export function QueryBuilder({
  condition,
  onChange,
  className,
}: QueryBuilderProps) {
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
  const effectiveCondition = useMemo<RuleCondition>(() => {
    if (!condition) {
      return {
        operator: "AND",
        conditions: [
          {
            field: "NAME",
            operator: "CONTAINS",
            value: "",
          },
        ],
      };
    }
    // Wrap non-group conditions in a group
    if (condition.operator !== "AND" && condition.operator !== "OR") {
      return {
        operator: "AND",
        conditions: [condition],
      };
    }
    return condition;
  }, [condition]);

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
      const { active, over } = event;
      if (!over || active.id === over.id) return;

      const activeIdStr = active.id as string;
      const overIdStr = over.id as string;

      // Parse paths from IDs (format: "root-0-1-2")
      const activePath = parseIdPath(activeIdStr);
      const overPath = parseIdPath(overIdStr);

      // Only handle reordering within the same parent
      if (!pathsHaveSameParent(activePath, overPath)) return;

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

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragEnd={handleDragEnd}
    >
      <div className={className}>
        <ConditionGroup
          id="root"
          condition={effectiveCondition}
          onChange={handleChange}
          isRoot
        />
      </div>
    </DndContext>
  );
}

// Helper: Parse ID path like "root-0-1" to [0, 1]
function parseIdPath(id: string): number[] {
  const parts = id.split("-");
  return parts.slice(1).map((p) => parseInt(p, 10));
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
  if (parentPath.length === 0) {
    // Reorder at root level
    if (!root.conditions) return null;
    const newConditions = arrayMove(root.conditions, fromIndex, toIndex);
    return { ...root, conditions: newConditions };
  }

  // Navigate to parent
  const newRoot = { ...root };
  let current = newRoot;

  for (let i = 0; i < parentPath.length - 1; i++) {
    const index = parentPath[i];
    if (!current.conditions?.[index]) return null;
    current.conditions = [...current.conditions];
    current.conditions[index] = { ...current.conditions[index] };
    current = current.conditions[index];
  }

  const lastIndex = parentPath[parentPath.length - 1];
  if (!current.conditions?.[lastIndex]?.conditions) return null;

  current.conditions = [...current.conditions];
  current.conditions[lastIndex] = {
    ...current.conditions[lastIndex],
    conditions: arrayMove(
      current.conditions[lastIndex].conditions!,
      fromIndex,
      toIndex
    ),
  };

  return newRoot;
}
