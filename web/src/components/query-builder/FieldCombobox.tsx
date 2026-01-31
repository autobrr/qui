/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
import { CONDITION_FIELDS, FIELD_GROUPS, type DisabledField } from "./constants";
import { DisabledOption } from "./DisabledOption";

interface FieldComboboxProps {
  value: string;
  onChange: (value: string) => void;
  disabledFields?: DisabledField[];
}

export function FieldCombobox({ value, onChange, disabledFields }: FieldComboboxProps) {
  const [open, setOpen] = useState(false);

  const selectedField = value ? CONDITION_FIELDS[value as keyof typeof CONDITION_FIELDS] : null;

  // Check if a field is disabled and get its reason
  const getDisabledReason = (field: string): string | null => {
    const disabled = disabledFields?.find(d => d.field === field);
    return disabled?.reason ?? null;
  };

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
            {FIELD_GROUPS.map((group) => (
              <CommandGroup key={group.label} heading={group.label}>
                {group.fields.map((field) => {
                  const fieldDef = CONDITION_FIELDS[field as keyof typeof CONDITION_FIELDS];
                  const disabledReason = getDisabledReason(field);
                  const isDisabled = disabledReason !== null;

                  if (isDisabled) {
                    return (
                      <DisabledOption key={field} reason={disabledReason}>
                        <CommandItem value={`${fieldDef?.label ?? field} ${group.label}`}>
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
                      </DisabledOption>
                    );
                  }

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
