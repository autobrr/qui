import type { ReactNode } from "react"

import {
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger
} from "@/components/ui/dropdown-menu"
import type { UnifiedActionInstance } from "@/hooks/useUnifiedActionInstances"
import { cn } from "@/lib/utils"
import { Cog, FileEdit, HardDrive, ListTodo, Plus } from "lucide-react"

interface UnifiedActionMenuSectionProps {
  manageableInstances: UnifiedActionInstance[]
  torrentCreationInstances: UnifiedActionInstance[]
  onSelectAddTorrentInstance: (id: number) => void
  onSelectCreateTorrentInstance: (id: number) => void
  onSelectTasksInstance: (id: number) => void
  onSelectSettingsInstance: (id: number) => void
}

interface UnifiedActionSubmenuProps {
  icon: ReactNode
  label: string
  instances: UnifiedActionInstance[]
  onSelectInstance: (id: number) => void
}

function UnifiedActionSubmenu({
  icon,
  label,
  instances,
  onSelectInstance,
}: UnifiedActionSubmenuProps) {
  if (instances.length === 0) {
    return null
  }

  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        {icon}
        {label}
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="min-w-56">
        {instances.map((instance) => (
          <DropdownMenuItem
            key={instance.id}
            onSelect={() => onSelectInstance(instance.id)}
            className="cursor-pointer"
          >
            <HardDrive className="h-4 w-4 flex-shrink-0" />
            <span className="flex-1 truncate">{instance.name}</span>
            <span
              className={cn(
                "ml-2 h-2 w-2 rounded-full flex-shrink-0",
                instance.connected ? "bg-green-500" : "bg-red-500"
              )}
            />
          </DropdownMenuItem>
        ))}
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  )
}

export function UnifiedActionMenuSection({
  manageableInstances,
  torrentCreationInstances,
  onSelectAddTorrentInstance,
  onSelectCreateTorrentInstance,
  onSelectTasksInstance,
  onSelectSettingsInstance,
}: UnifiedActionMenuSectionProps) {
  if (manageableInstances.length === 0) {
    return null
  }

  return (
    <>
      <DropdownMenuSeparator />
      <DropdownMenuLabel className="text-xs uppercase tracking-wide text-muted-foreground">
        Actions
      </DropdownMenuLabel>
      <UnifiedActionSubmenu
        icon={<Plus className="h-4 w-4" />}
        label="Add torrent"
        instances={manageableInstances}
        onSelectInstance={onSelectAddTorrentInstance}
      />
      <UnifiedActionSubmenu
        icon={<FileEdit className="h-4 w-4" />}
        label="Create torrent"
        instances={torrentCreationInstances}
        onSelectInstance={onSelectCreateTorrentInstance}
      />
      <UnifiedActionSubmenu
        icon={<ListTodo className="h-4 w-4" />}
        label="Torrent creation tasks"
        instances={torrentCreationInstances}
        onSelectInstance={onSelectTasksInstance}
      />
      <UnifiedActionSubmenu
        icon={<Cog className="h-4 w-4" />}
        label="Instance settings"
        instances={manageableInstances}
        onSelectInstance={onSelectSettingsInstance}
      />
    </>
  )
}
