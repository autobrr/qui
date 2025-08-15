import os
from datetime import datetime

# --- Configuration ---
COPYRIGHT_HOLDER = "s0up and the autobrr contributors"
START_YEAR = 2025
CURRENT_YEAR = datetime.now().year
COPYRIGHT_YEAR = f"{START_YEAR}-{CURRENT_YEAR}" if CURRENT_YEAR > START_YEAR else str(START_YEAR)
LICENSE = "GPL-2.0-or-later"

# Excluded directories
EXCLUDED_DIRS = {'.git', 'node_modules', 'dist', 'build', 'vendor'}

# Header for TypeScript/TSX files (4 lines)
TS_HEADER = f"""/*
 * Copyright (c) {COPYRIGHT_YEAR}, {COPYRIGHT_HOLDER}.
 * SPDX-License-Identifier: {LICENSE}
 */
"""

# Header for Go files (2 lines)
GO_HEADER = f"""// Copyright (c) {COPYRIGHT_YEAR}, {COPYRIGHT_HOLDER}.
// SPDX-License-Identifier: {LICENSE}
"""

def has_copyright_header(lines, is_typescript=False):
    """Check if file already has a copyright header."""
    if not lines:
        return False
    
    if is_typescript:
        # Check for TypeScript/TSX copyright header (/* ... */)
        if len(lines) >= 4:
            return (lines[0].strip().startswith("/*") and 
                    any("Copyright" in line or "copyright" in line for line in lines[:4]) and
                    any("*/" in line for line in lines[:4]))
    else:
        # Check for Go copyright header (// Copyright ...)
        if len(lines) >= 2:
            return any("Copyright" in line or "copyright" in line for line in lines[:2])
    
    return False

def process_file(file_path):
    """Determines the correct header and overwrites the file."""
    header_to_apply = ""
    lines_to_remove = 0
    is_typescript = False

    if file_path.endswith((".ts", ".tsx")):
        header_to_apply = TS_HEADER
        lines_to_remove = 4
        is_typescript = True
    elif file_path.endswith(".go"):
        header_to_apply = GO_HEADER
        lines_to_remove = 2
        is_typescript = False
    else:
        return # Not a file we need to process

    print(f"CHECKING: {file_path}")

    try:
        # Read the original content first
        with open(file_path, 'r', encoding='utf-8') as f:
            original_lines = f.readlines()

        # Check if file already has a copyright header
        if has_copyright_header(original_lines, is_typescript):
            print(f"  UPDATING HEADER: {file_path}")
            # Remove old header
            remaining_lines = original_lines[lines_to_remove:]
        else:
            print(f"  ADDING HEADER: {file_path}")
            # Keep all original content
            remaining_lines = original_lines

        # Overwrite the file with the new content
        with open(file_path, 'w', encoding='utf-8') as f:
            f.write(header_to_apply)
            
            # Strip leading blank lines from remaining content
            while remaining_lines and remaining_lines[0].strip() == '':
                remaining_lines.pop(0)
            
            # Add exactly one blank line between header and content
            f.write('\n')
            
            # Write back the rest of the original file
            f.writelines(remaining_lines)

        return 1
    except Exception as e:
        print(f"  ERROR: Failed to process {file_path}: {e}")
        return 0

def main():
    """Finds and processes all relevant files."""
    import sys
    
    # Check if a specific file was provided as argument
    if len(sys.argv) > 1:
        file_path = sys.argv[1]
        print(f"Processing single file: {file_path}")
        result = process_file(file_path)
        if result:
            print(f"✅ Successfully updated {file_path}")
        else:
            print(f"❌ No update needed for {file_path}")
        return
    
    # Otherwise, process all files
    print("Starting header update process...")
    updated_count = 0
    
    # Walk through all directories and files starting from the current directory
    for root, dirs, files in os.walk('.'):
        # Modify the dir list in-place to prevent os.walk from descending into excluded folders
        dirs[:] = [d for d in dirs if d not in EXCLUDED_DIRS]
        
        for file in files:
            file_path = os.path.join(root, file)
            result = process_file(file_path)
            if result:
                updated_count += result

    print("\n=== Summary ===")
    print(f"Files updated: {updated_count}")
    print("\nAll done! Review and commit the changes. ✅")

if __name__ == "__main__":
    main()