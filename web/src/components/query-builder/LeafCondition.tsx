import { useState } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { format } from "date-fns";
import { GripVertical, X, ToggleLeft, ToggleRight, CalendarIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
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
  getFieldType,
  getOperatorsForField,
  TORRENT_STATES,
  BYTE_UNITS,
  SPEED_UNITS,
} from "./constants";
import { FieldCombobox } from "./FieldCombobox";

const DURATION_INPUT_UNITS = [
  { value: 60, label: "minutes" },
  { value: 3600, label: "hours" },
  { value: 86400, label: "days" },
];

interface LeafConditionProps {
  id: string;
  condition: RuleCondition;
  onChange: (condition: RuleCondition) => void;
  onRemove: () => void;
  isOnly?: boolean;
  /** Optional category options for EXISTS_IN/CONTAINS_IN operators */
  categoryOptions?: Array<{ label: string; value: string }>;
}

export function LeafCondition({
  id,
  condition,
  onChange,
  onRemove,
  isOnly,
  categoryOptions,
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

  // Track duration unit separately so it persists when value is empty
  const [durationUnit, setDurationUnit] = useState<number>(() => {
    // Initialize from existing value if present
    const secs = parseFloat(condition.value ?? "0") || 0;
    if (secs >= 86400 && secs % 86400 === 0) return 86400;
    if (secs >= 3600 && secs % 3600 === 0) return 3600;
    return 60;
  });

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

  // Duration handling - parse seconds to display value using tracked unit
  const getDurationDisplay = (): { value: string; unit: number } => {
    const secs = parseFloat(condition.value ?? "0") || 0;
    if (secs === 0) return { value: "", unit: durationUnit };
    return { value: String(secs / durationUnit), unit: durationUnit };
  };

  const durationDisplay = fieldType === "duration" ? getDurationDisplay() : null;

  const handleDurationChange = (value: string, unit: number) => {
    // Always update the unit preference
    setDurationUnit(unit);
    // Only update condition value if there's an actual value
    if (value === "") {
      onChange({ ...condition, value: "" });
    } else {
      const numValue = parseFloat(value) || 0;
      const seconds = Math.round(numValue * unit);
      onChange({ ...condition, value: String(seconds) });
    }
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
      <FieldCombobox value={condition.field ?? ""} onChange={handleFieldChange} />

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
      {condition.operator === "BETWEEN" && fieldType === "timestamp" ? (
        <div className="flex items-center gap-1">
          <Popover>
            <PopoverTrigger asChild>
              <Button
                type="button"
                variant="outline"
                className={cn(
                  "h-8 w-[130px] justify-start text-left font-normal text-xs",
                  !condition.minValue && "text-muted-foreground"
                )}
              >
                <CalendarIcon className="mr-1 size-3" />
                {condition.minValue
                  ? format(new Date(condition.minValue * 1000), "PP")
                  : "From"}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-auto p-0" align="start">
              <Calendar
                mode="single"
                selected={condition.minValue ? new Date(condition.minValue * 1000) : undefined}
                onSelect={(date) => {
                  onChange({ ...condition, minValue: date ? Math.floor(date.getTime() / 1000) : undefined });
                }}
              />
            </PopoverContent>
          </Popover>
          <span className="text-muted-foreground">-</span>
          <Popover>
            <PopoverTrigger asChild>
              <Button
                type="button"
                variant="outline"
                className={cn(
                  "h-8 w-[130px] justify-start text-left font-normal text-xs",
                  !condition.maxValue && "text-muted-foreground"
                )}
              >
                <CalendarIcon className="mr-1 size-3" />
                {condition.maxValue
                  ? format(new Date(condition.maxValue * 1000), "PP")
                  : "To"}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-auto p-0" align="start">
              <Calendar
                mode="single"
                selected={condition.maxValue ? new Date(condition.maxValue * 1000) : undefined}
                onSelect={(date) => {
                  onChange({ ...condition, maxValue: date ? Math.floor(date.getTime() / 1000) : undefined });
                }}
              />
            </PopoverContent>
          </Popover>
        </div>
      ) : condition.operator === "BETWEEN" ? (
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
      ) : fieldType === "timestamp" ? (
        <Popover>
          <PopoverTrigger asChild>
            <Button
              type="button"
              variant="outline"
              className={cn(
                "h-8 w-[160px] justify-start text-left font-normal",
                !condition.value && "text-muted-foreground"
              )}
            >
              <CalendarIcon className="mr-2 size-4" />
              {condition.value
                ? format(new Date(parseInt(condition.value, 10) * 1000), "PP")
                : "Pick a date"}
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-auto p-0" align="start">
            <Calendar
              mode="single"
              selected={condition.value ? new Date(parseInt(condition.value, 10) * 1000) : undefined}
              onSelect={(date) => {
                handleValueChange(date ? String(Math.floor(date.getTime() / 1000)) : "");
              }}
            />
          </PopoverContent>
        </Popover>
      ) : (condition.operator === "EXISTS_IN" || condition.operator === "CONTAINS_IN" || (condition.field === "CATEGORY" && (condition.operator === "EQUAL" || condition.operator === "NOT_EQUAL"))) && categoryOptions && categoryOptions.length > 0 ? (
        // Category selector for category-related conditions when categories available
        <Select value={condition.value ?? ""} onValueChange={handleValueChange}>
          <SelectTrigger className="h-8 w-[160px]">
            <SelectValue placeholder="Select category" />
          </SelectTrigger>
          <SelectContent>
            {categoryOptions.map((cat) => (
              <SelectItem key={cat.value} value={cat.value}>
                {cat.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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
          {/* Regex toggle for string fields - hide for EXISTS_IN/CONTAINS_IN */}
          {fieldType === "string" &&
            condition.operator !== "MATCHES" &&
            condition.operator !== "EXISTS_IN" &&
            condition.operator !== "CONTAINS_IN" && (
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
