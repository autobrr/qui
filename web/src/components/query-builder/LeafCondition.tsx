import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, X, ToggleLeft, ToggleRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { RuleCondition, ConditionField, ConditionOperator } from "@/types";
import {
  CONDITION_FIELDS,
  FIELD_GROUPS,
  getFieldType,
  getOperatorsForField,
  TORRENT_STATES,
  BYTE_UNITS,
  SPEED_UNITS,
} from "./constants";

const DURATION_INPUT_UNITS = [
  { value: 60, label: "minutes" },
  { value: 86400, label: "days" },
];

interface LeafConditionProps {
  id: string;
  condition: RuleCondition;
  onChange: (condition: RuleCondition) => void;
  onRemove: () => void;
  isOnly?: boolean;
}

export function LeafCondition({
  id,
  condition,
  onChange,
  onRemove,
  isOnly,
}: LeafConditionProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    isDragging,
  } = useSortable({ id });

  const style = {
    transform: CSS.Translate.toString(transform),
  };

  const fieldType = condition.field ? getFieldType(condition.field) : "string";
  const operators = condition.field ? getOperatorsForField(condition.field) : [];

  const handleFieldChange = (field: string) => {
    const newFieldType = getFieldType(field);
    const newOperators = getOperatorsForField(field);
    const defaultOperator = newOperators[0]?.value ?? "EQUAL";

    onChange({
      ...condition,
      field: field as ConditionField,
      operator: defaultOperator as ConditionOperator,
      value: newFieldType === "boolean" ? "true" : "",
      minValue: undefined,
      maxValue: undefined,
    });
  };

  const handleOperatorChange = (operator: string) => {
    onChange({
      ...condition,
      operator: operator as ConditionOperator,
      minValue: operator === "BETWEEN" ? 0 : undefined,
      maxValue: operator === "BETWEEN" ? 0 : undefined,
    });
  };

  const handleValueChange = (value: string) => {
    onChange({ ...condition, value });
  };

  const handleMinValueChange = (value: string) => {
    onChange({ ...condition, minValue: parseFloat(value) || 0 });
  };

  const handleMaxValueChange = (value: string) => {
    onChange({ ...condition, maxValue: parseFloat(value) || 0 });
  };

  const toggleNegate = () => {
    onChange({ ...condition, negate: !condition.negate });
  };

  const toggleRegex = () => {
    onChange({ ...condition, regex: !condition.regex });
  };

  // Duration handling - parse seconds to display value/unit
  const parseDuration = (seconds: string | undefined): { value: string; unit: number } => {
    const secs = parseFloat(seconds ?? "0") || 0;
    if (secs === 0) return { value: "", unit: 60 }; // default to minutes
    // Prefer days if evenly divisible, else minutes
    if (secs >= 86400 && secs % 86400 === 0) {
      return { value: String(secs / 86400), unit: 86400 };
    }
    return { value: String(secs / 60), unit: 60 };
  };

  const durationDisplay = fieldType === "duration" ? parseDuration(condition.value) : null;

  const handleDurationChange = (value: string, unit: number) => {
    const numValue = parseFloat(value) || 0;
    const seconds = Math.round(numValue * unit);
    onChange({ ...condition, value: String(seconds) });
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn(
        "flex items-center gap-2 rounded-md border bg-card p-2",
        isDragging && "opacity-50",
        condition.negate && "border-destructive/50"
      )}
    >
      {/* Drag handle */}
      <button
        type="button"
        className="cursor-grab touch-none text-muted-foreground hover:text-foreground"
        {...attributes}
        {...listeners}
      >
        <GripVertical className="size-4" />
      </button>

      {/* Negate toggle */}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className={cn(
              "h-7 px-2 text-xs",
              condition.negate && "bg-destructive/10 text-destructive"
            )}
            onClick={toggleNegate}
          >
            {condition.negate ? "NOT" : "IF"}
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          {condition.negate ? "Condition is negated" : "Click to negate"}
        </TooltipContent>
      </Tooltip>

      {/* Field selector */}
      <Select value={condition.field ?? ""} onValueChange={handleFieldChange}>
        <SelectTrigger className="h-8 w-[140px]">
          <SelectValue placeholder="Select field" />
        </SelectTrigger>
        <SelectContent>
          {FIELD_GROUPS.map((group) => (
            <SelectGroup key={group.label}>
              <SelectLabel>{group.label}</SelectLabel>
              {group.fields.map((field) => {
                const fieldDef = CONDITION_FIELDS[field as keyof typeof CONDITION_FIELDS];
                return (
                  <SelectItem key={field} value={field}>
                    {fieldDef?.label ?? field}
                  </SelectItem>
                );
              })}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>

      {/* Operator selector */}
      <Select
        value={condition.operator ?? ""}
        onValueChange={handleOperatorChange}
        disabled={!condition.field}
      >
        <SelectTrigger className="h-8 w-[120px]">
          <SelectValue placeholder="Operator" />
        </SelectTrigger>
        <SelectContent>
          {operators.map((op) => (
            <SelectItem key={op.value} value={op.value}>
              {op.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Value input - varies by field type */}
      {condition.operator === "BETWEEN" ? (
        <div className="flex items-center gap-1">
          <Input
            type="number"
            className="h-8 w-20"
            value={condition.minValue ?? ""}
            onChange={(e) => handleMinValueChange(e.target.value)}
            placeholder="Min"
          />
          <span className="text-muted-foreground">-</span>
          <Input
            type="number"
            className="h-8 w-20"
            value={condition.maxValue ?? ""}
            onChange={(e) => handleMaxValueChange(e.target.value)}
            placeholder="Max"
          />
          {renderUnitHint(fieldType)}
        </div>
      ) : fieldType === "state" ? (
        <Select value={condition.value ?? ""} onValueChange={handleValueChange}>
          <SelectTrigger className="h-8 w-[160px]">
            <SelectValue placeholder="Select state" />
          </SelectTrigger>
          <SelectContent>
            {TORRENT_STATES.map((state) => (
              <SelectItem key={state.value} value={state.value}>
                {state.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : fieldType === "boolean" ? (
        <Select value={condition.value ?? "true"} onValueChange={handleValueChange}>
          <SelectTrigger className="h-8 w-[100px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="true">True</SelectItem>
            <SelectItem value="false">False</SelectItem>
          </SelectContent>
        </Select>
      ) : fieldType === "duration" && durationDisplay ? (
        <div className="flex items-center gap-1">
          <Input
            type="number"
            className="h-8 w-20"
            value={durationDisplay.value}
            onChange={(e) => handleDurationChange(e.target.value, durationDisplay.unit)}
            placeholder="0"
          />
          <Select
            value={String(durationDisplay.unit)}
            onValueChange={(unit) => handleDurationChange(durationDisplay.value, parseInt(unit, 10))}
          >
            <SelectTrigger className="h-8 w-[100px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {DURATION_INPUT_UNITS.map((u) => (
                <SelectItem key={u.value} value={String(u.value)}>
                  {u.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : (
        <div className="flex items-center gap-1">
          <Input
            type={isNumericType(fieldType) ? "number" : "text"}
            className="h-8 w-32 flex-1"
            value={condition.value ?? ""}
            onChange={(e) => handleValueChange(e.target.value)}
            placeholder={getPlaceholder(fieldType)}
          />
          {renderUnitHint(fieldType)}
          {/* Regex toggle for string fields */}
          {fieldType === "string" && condition.operator !== "MATCHES" && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className={cn(
                    "h-7 px-2",
                    condition.regex && "bg-primary/10 text-primary"
                  )}
                  onClick={toggleRegex}
                >
                  {condition.regex ? (
                    <ToggleRight className="size-4" />
                  ) : (
                    <ToggleLeft className="size-4" />
                  )}
                  <span className="ml-1 text-xs">.*</span>
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {condition.regex ? "Regex enabled" : "Enable regex"}
              </TooltipContent>
            </Tooltip>
          )}
        </div>
      )}

      {/* Remove button */}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="ml-auto h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
        onClick={onRemove}
        disabled={isOnly}
      >
        <X className="size-4" />
      </Button>
    </div>
  );
}

function isNumericType(type: string): boolean {
  return ["bytes", "duration", "timestamp", "float", "speed", "integer"].includes(type);
}

function getPlaceholder(type: string): string {
  switch (type) {
    case "bytes":
      return "Size in bytes";
    case "duration":
      return "Seconds";
    case "timestamp":
      return "Unix timestamp";
    case "float":
      return "0.0";
    case "speed":
      return "Bytes/s";
    case "integer":
      return "0";
    default:
      return "Value";
  }
}

function renderUnitHint(type: string) {
  const units = getUnitsForType(type);
  if (!units) return null;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-help text-xs text-muted-foreground">
          {units[0].label}
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        <div className="space-y-1">
          <p className="font-medium">Unit conversions:</p>
          {units.map((u: { value: number; label: string }) => (
            <p key={u.label}>
              1 {u.label} = {u.value.toLocaleString()} base units
            </p>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

function getUnitsForType(type: string) {
  switch (type) {
    case "bytes":
      return BYTE_UNITS;
    case "speed":
      return SPEED_UNITS;
    default:
      return null;
  }
}
