/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { useForm } from "@tanstack/react-form"
import { useCustomContextMenuItems } from "@/hooks/useCustomContextMenuItems"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Plus, Trash2, Edit, Settings, ExternalLink, ChevronDown, ChevronRight } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { toast } from "sonner"
import type { CustomContextMenuItem } from "@/types/contextMenu"
import { DEFAULT_CUSTOM_MENU_ITEM, TORRENT_VARIABLES } from "@/types/contextMenu"

export function ContextMenuSettingsManager() {
  const { customMenuItems, saveMenuItems } = useCustomContextMenuItems()
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [editingItem, setEditingItem] = useState<CustomContextMenuItem | null>(null)
  const [deleteItemId, setDeleteItemId] = useState<string | null>(null)
  const [showVariableHelp, setShowVariableHelp] = useState(false)

  const createForm = useForm({
    defaultValues: DEFAULT_CUSTOM_MENU_ITEM,
    onSubmit: async ({ value }) => {
      // In command line mode, arguments (command) is required; in direct mode, executable is required
      const executableRequired = !value.useCommandLine
      const commandRequired = value.useCommandLine
      
      if (!value.name.trim()) {
        toast.error("Name is required")
        return
      }
      
      if (executableRequired && !value.executable.trim()) {
        toast.error("Executable path is required")
        return
      }
      
      if (commandRequired && !value.arguments.trim()) {
        toast.error("Command is required")
        return
      }

      const newItem: CustomContextMenuItem = {
        ...value,
        id: `custom-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
        name: value.name.trim(),
        executable: value.executable.trim(),
        arguments: value.arguments.trim(),
        pathMapping: value.pathMapping.trim(),
        enabled: value.enabled,
        highPrivileges: value.highPrivileges,
        useCommandLine: value.useCommandLine,
        keepTerminalOpen: value.keepTerminalOpen,
      }

      const updatedItems = [...customMenuItems, newItem]
      saveMenuItems(updatedItems)
      createForm.reset()
      setShowCreateDialog(false)
      toast.success("Custom menu item created successfully")
    },
  })

  const editForm = useForm({
    defaultValues: DEFAULT_CUSTOM_MENU_ITEM,
    onSubmit: async ({ value }) => {
      // In command line mode, arguments (command) is required; in direct mode, executable is required
      const executableRequired = !value.useCommandLine
      const commandRequired = value.useCommandLine
      
      if (!editingItem || !value.name.trim()) {
        toast.error("Name is required")
        return
      }
      
      if (executableRequired && !value.executable.trim()) {
        toast.error("Executable path is required")
        return
      }
      
      if (commandRequired && !value.arguments.trim()) {
        toast.error("Command is required")
        return
      }

      const updatedItem: CustomContextMenuItem = {
        ...editingItem,
        name: value.name.trim(),
        executable: value.executable.trim(),
        arguments: value.arguments.trim(),
        pathMapping: value.pathMapping.trim(),
        enabled: value.enabled,
        highPrivileges: value.highPrivileges,
        useCommandLine: value.useCommandLine,
        keepTerminalOpen: value.keepTerminalOpen,
      }

      const updatedItems = customMenuItems.map(item => 
        item.id === editingItem.id ? updatedItem : item
      )
      saveMenuItems(updatedItems)
      editForm.reset()
      setEditingItem(null)
      toast.success("Custom menu item updated successfully")
    },
  })

  const handleEdit = (item: CustomContextMenuItem) => {
    setEditingItem(item)
    editForm.setFieldValue('name', item.name)
    editForm.setFieldValue('executable', item.executable)
    editForm.setFieldValue('arguments', item.arguments)
    editForm.setFieldValue('pathMapping', item.pathMapping)
    editForm.setFieldValue('enabled', item.enabled)
    editForm.setFieldValue('highPrivileges', item.highPrivileges)
    editForm.setFieldValue('useCommandLine', item.useCommandLine)
    editForm.setFieldValue('keepTerminalOpen', item.keepTerminalOpen)
  }

  const handleDelete = (itemId: string) => {
    const updatedItems = customMenuItems.filter(item => item.id !== itemId)
    saveMenuItems(updatedItems)
    setDeleteItemId(null)
    toast.success("Custom menu item deleted successfully")
  }

  const toggleItemEnabled = (itemId: string) => {
    const updatedItems = customMenuItems.map(item =>
      item.id === itemId ? { ...item, enabled: !item.enabled } : item
    )
    saveMenuItems(updatedItems)
  }

  const renderMenuItemForm = (
    form: typeof createForm,
    isEdit: boolean = false
  ) => (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        e.stopPropagation()
        form.handleSubmit()
      }}
      className="space-y-4"
    >
      <div className="grid gap-4">
        <form.Field name="name">
          {(field) => (
            <div className="space-y-2">
              <Label htmlFor={`${isEdit ? 'edit' : 'create'}-name`} className="text-sm font-medium">
                Menu Item Name <span className="text-destructive">*</span>
              </Label>
              <Input
                id={`${isEdit ? 'edit' : 'create'}-name`}
                placeholder="e.g., Open in VLC"
                value={field.state.value}
                onChange={(e) => field.handleChange(e.target.value)}
                onBlur={field.handleBlur}
              />
              {field.state.meta.errors && (
                <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Subscribe selector={(state) => state.values.useCommandLine}>
          {(useCommandLineValue) => (
            <>
              <form.Field name="executable">
                {(field) => {
                  // Hide executable field when in command line mode
                  if (useCommandLineValue) {
                    return null
                  }
                  
                  return (
                    <div className="space-y-2">
                      <Label htmlFor={`${isEdit ? 'edit' : 'create'}-executable`} className="text-sm font-medium">
                        Executable Path <span className="text-destructive">*</span>
                      </Label>
                      <Input
                        id={`${isEdit ? 'edit' : 'create'}-executable`}
                        placeholder="e.g., C:\\Program Files\\VideoLAN\\VLC\\vlc.exe"
                        value={field.state.value}
                        onChange={(e) => field.handleChange(e.target.value)}
                        onBlur={field.handleBlur}
                      />
                      <p className="text-xs text-muted-foreground">
                        Full path to the executable program
                      </p>
                      {field.state.meta.errors && (
                        <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                      )}
                    </div>
                  )
                }}
              </form.Field>

              <form.Field name="arguments">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={`${isEdit ? 'edit' : 'create'}-arguments`} className="text-sm font-medium">
                      {useCommandLineValue ? 'Command' : 'Additional Arguments'}
                      {useCommandLineValue && <span className="text-destructive"> *</span>}
                    </Label>
                    <Input
                      id={`${isEdit ? 'edit' : 'create'}-arguments`}
                      placeholder={useCommandLineValue 
                        ? "e.g., py script.py {torrent.save_path} --arg" 
                        : "e.g., {torrent.save_path} --fullscreen -ua"}
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      onBlur={field.handleBlur}
                    />
                    <div className="space-y-1">
                      <p className="text-xs text-muted-foreground">
                        {useCommandLineValue 
                          ? "Full command to execute (e.g., 'py script.py' or 'notepad file.txt'). You can use torrent variables like {torrent.save_path}, {torrent.hash}, {torrent.name}, etc."
                          : "Optional command line arguments. You can use torrent variables like {torrent.save_path}, {torrent.hash}, {torrent.name}, etc."}
                      </p>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-auto p-1 text-xs text-muted-foreground hover:text-foreground"
                        onClick={() => setShowVariableHelp(!showVariableHelp)}
                      >
                        {showVariableHelp ? <ChevronDown className="mr-1 h-3 w-3" /> : <ChevronRight className="mr-1 h-3 w-3" />}
                        View all available variables
                      </Button>
                      {showVariableHelp && (
                        <div className="mt-2 p-3 bg-muted/30 rounded-md border">
                          <h4 className="text-xs font-medium mb-2">Available Torrent Variables:</h4>
                          <div className="grid grid-cols-1 gap-1 text-xs">
                            {Object.entries(TORRENT_VARIABLES).map(([variable, description]) => (
                              <div key={variable} className="flex justify-between">
                                <code className="text-primary">{`{${variable}}`}</code>
                                <span className="text-muted-foreground ml-2">{description}</span>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                    {field.state.meta.errors && (
                      <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                    )}
                  </div>
                )}
              </form.Field>
            </>
          )}
        </form.Subscribe>

        <form.Field name="pathMapping">
          {(field) => (
            <div className="space-y-2">
              <Label htmlFor={`${isEdit ? 'edit' : 'create'}-pathMapping`} className="text-sm font-medium">
                Path Mapping
              </Label>
              <Input
                id={`${isEdit ? 'edit' : 'create'}-pathMapping`}
                placeholder="e.g., /downloads/:{pathSeparator}\\Downloads\\"
                value={field.state.value}
                onChange={(e) => field.handleChange(e.target.value)}
                onBlur={field.handleBlur}
              />
              <p className="text-xs text-muted-foreground">
                Optional path transformation from server path to local path. Use {"{pathSeparator}"} for OS-specific separators.
              </p>
              {field.state.meta.errors && (
                <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Field name="useCommandLine">
          {(field) => (
            <div className="space-y-2">
              <div className="flex items-center space-x-2">
                <Switch
                  id={`${isEdit ? 'edit' : 'create'}-useCommandLine`}
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                />
                <Label htmlFor={`${isEdit ? 'edit' : 'create'}-useCommandLine`} className="text-sm font-medium">
                  Use Command Line Mode
                </Label>
              </div>
              <p className="text-xs text-muted-foreground">
                {field.state.value 
                  ? "Execute as command line arguments in cmd (e.g., 'py script.py' or 'notepad file.txt')" 
                  : "Execute as direct application path (e.g., 'C:\\Program Files\\App\\app.exe')"}
              </p>
            </div>
          )}
        </form.Field>

        <form.Subscribe selector={(state) => state.values.useCommandLine}>
          {(useCommandLineValue) => (
            <form.Field name="keepTerminalOpen">
              {(field) => {
                // Only show this toggle when in command line mode
                if (!useCommandLineValue) {
                  return null
                }
                
                return (
                  <div className="space-y-2">
                    <div className="flex items-center space-x-2">
                      <Switch
                        id={`${isEdit ? 'edit' : 'create'}-keepTerminalOpen`}
                        checked={field.state.value}
                        onCheckedChange={field.handleChange}
                      />
                      <Label htmlFor={`${isEdit ? 'edit' : 'create'}-keepTerminalOpen`} className="text-sm font-medium">
                        Keep Terminal Open
                      </Label>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {field.state.value 
                        ? "Terminal will remain open after command execution (cmd /k)" 
                        : "Terminal will close automatically after command execution (cmd /c)"}
                    </p>
                  </div>
                )
              }}
            </form.Field>
          )}
        </form.Subscribe>

        <form.Field name="highPrivileges">
          {(field) => (
            <div className="space-y-2">
              <div className="flex items-center space-x-2">
                <Switch
                  id={`${isEdit ? 'edit' : 'create'}-highPrivileges`}
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                />
                <Label htmlFor={`${isEdit ? 'edit' : 'create'}-highPrivileges`} className="text-sm font-medium">
                  High Privileges (Windows)
                </Label>
              </div>
              {field.state.value && (
                <p className="text-xs text-muted-foreground">
                  On Windows, this will launch the application with high priority using 'start /high'
                </p>
              )}
            </div>
          )}
        </form.Field>

        {isEdit && (
          <form.Field name="enabled">
            {(field) => (
              <div className="flex items-center space-x-2">
                <Switch
                  id={`${isEdit ? 'edit' : 'create'}-enabled`}
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                />
                <Label htmlFor={`${isEdit ? 'edit' : 'create'}-enabled`} className="text-sm font-medium">
                  Enabled
                </Label>
              </div>
            )}
          </form.Field>
        )}
      </div>

      <div className="flex justify-end space-x-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            form.reset()
            if (isEdit) {
              setEditingItem(null)
            } else {
              setShowCreateDialog(false)
            }
          }}
        >
          Cancel
        </Button>
        <Button type="submit" disabled={form.state.isSubmitting}>
          {isEdit ? 'Update' : 'Create'} Menu Item
        </Button>
      </div>
    </form>
  )

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between">
          <div className="space-y-1.5">
            <CardTitle className="flex items-center gap-2">
              <Settings className="h-5 w-5" />
              Custom Context Menu
            </CardTitle>
            <CardDescription>
              Add custom menu items to the torrent context menu for launching external programs
            </CardDescription>
          </div>
          <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
            <DialogTrigger asChild>
              <Button size="sm">
                <Plus className="mr-2 h-4 w-4" />
                Add Menu Item
              </Button>
            </DialogTrigger>
            <DialogContent className="sm:max-w-2xl">
              <DialogHeader>
                <DialogTitle>Create Custom Menu Item</DialogTitle>
                <DialogDescription>
                  Add a new item to the torrent context menu that will launch an external program.
                </DialogDescription>
              </DialogHeader>
              {renderMenuItemForm(createForm)}
            </DialogContent>
          </Dialog>
        </div>
      </CardHeader>
      <CardContent>
        {customMenuItems.length === 0 ? (
          <div className="text-center py-8 text-muted-foreground">
            <ExternalLink className="mx-auto h-12 w-12 mb-4 opacity-50" />
            <h3 className="text-lg font-medium mb-2">No custom menu items</h3>
            <p className="text-sm mb-4">
              Create custom menu items to launch external programs from the torrent context menu
            </p>
            <Button onClick={() => setShowCreateDialog(true)}>
              <Plus className="mr-2 h-4 w-4" />
              Add Your First Menu Item
            </Button>
          </div>
        ) : (
          <div className="space-y-3">
            {customMenuItems.map((item) => (
              <Card key={item.id} className="p-4">
                <div className="flex items-start justify-between">
                  <div className="flex-1 space-y-1">
                    <div className="flex items-center gap-2">
                      <h4 className="text-sm font-medium">{item.name}</h4>
                      <Badge variant={item.enabled ? "default" : "secondary"}>
                        {item.enabled ? "Enabled" : "Disabled"}
                      </Badge>
                    </div>
                    <p className="text-xs text-muted-foreground font-mono bg-muted px-2 py-1 rounded">
                      {item.executable}
                      {item.arguments && ` ${item.arguments}`}
                    </p>
                    {item.pathMapping && (
                      <p className="text-xs text-muted-foreground">
                        <span className="font-medium">Path mapping:</span> {item.pathMapping}
                      </p>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={item.enabled}
                      onCheckedChange={() => toggleItemEnabled(item.id)}
                    />
                    <Button
                      size="icon"
                      variant="outline"
                      className="h-8 w-8"
                      onClick={() => handleEdit(item)}
                      title="Edit menu item"
                    >
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      size="icon"
                      variant="outline"
                      className="h-8 w-8 text-destructive hover:text-destructive"
                      onClick={() => setDeleteItemId(item.id)}
                      title="Delete menu item"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </Card>
            ))}
          </div>
        )}

        <div className="mt-6 p-4 bg-muted/30 rounded-lg">
          <h4 className="text-sm font-medium mb-2">Usage Information</h4>
          <ul className="text-xs text-muted-foreground space-y-1">
            <li>• Custom menu items will appear in the torrent right-click context menu</li>
            <li>• Use torrent variables in arguments like {"{torrent.save_path}"}, {"{torrent.hash}"}, {"{torrent.name}"}</li>
            <li>• The torrent's file path will be automatically added if not included in arguments</li>
            <li>• Use path mapping to convert server paths to local paths if needed</li>
            <li>• {"{pathSeparator}"} in path mapping will be replaced with the OS-appropriate path separator</li>
            <li>• Variables are replaced with actual torrent data when the menu item is clicked</li>
          </ul>
        </div>
      </CardContent>

      {/* Edit Dialog */}
      <Dialog open={!!editingItem} onOpenChange={(open) => !open && setEditingItem(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Edit Menu Item</DialogTitle>
            <DialogDescription>
              Update the custom menu item configuration.
            </DialogDescription>
          </DialogHeader>
          {editingItem && renderMenuItemForm(editForm, true)}
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={!!deleteItemId} onOpenChange={(open) => !open && setDeleteItemId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Menu Item</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete this custom menu item? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteItemId && handleDelete(deleteItemId)}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}