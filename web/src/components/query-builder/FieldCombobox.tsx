import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { Check, ChevronsUpDown } from "lucide-react";
import { useState } from "react";
import { CONDITION_FIELDS, FIELD_GROUPS } from "./constants";

interface FieldComboboxProps {
  value: string;
  onChange: (value: string) => void;
  hiddenFields?: string[];
}

export function FieldCombobox({ value, onChange, hiddenFields }: FieldComboboxProps) {
  const [open, setOpen] = useState(false);

  const selectedField = value? CONDITION_FIELDS[value as keyof typeof CONDITION_FIELDS]: null;

  const visibleGroups = FIELD_GROUPS
    .map(group => ({
      ...group,
      fields: group.fields.filter(field => !hiddenFields?.includes(field)),
    }))
    .filter(group => group.fields.length > 0);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="h-8 w-fit min-w-[120px] justify-between px-2 text-xs font-normal"
        >
          <span>
            {selectedField?.label ?? "Select field"}
          </span>
          <ChevronsUpDown className="ml-1 size-3 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-0" align="start">
        <Command>
          <CommandInput placeholder="Search fields..." className="h-9" />
          <CommandList>
            <CommandEmpty>No field found.</CommandEmpty>
            {visibleGroups.map((group) => (
              <CommandGroup key={group.label} heading={group.label}>
                {group.fields.map((field) => {
                  const fieldDef = CONDITION_FIELDS[field as keyof typeof CONDITION_FIELDS];
                  return (
                    <CommandItem
                      key={field}
                      value={`${fieldDef?.label ?? field} ${group.label}`}
                      onSelect={() => {
                        onChange(field);
                        setOpen(false);
                      }}
                    >
                      <Check
                        className={cn(
                          "mr-2 size-3",
                          value === field ? "opacity-100" : "opacity-0"
                        )}
                      />
                      <span>{fieldDef?.label ?? field}</span>
                      <span className="ml-auto text-[10px] text-muted-foreground">
                        {fieldDef?.type}
                      </span>
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
