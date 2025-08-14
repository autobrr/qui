import os

# --- Configuration ---
COPYRIGHT_HOLDER = "s0up and the autobrr contributors"
COPYRIGHT_YEAR = "2025"
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

def process_file(file_path):
    """Determines the correct header and overwrites the file."""
    header_to_apply = ""
    lines_to_remove = 0

    if file_path.endswith((".ts", ".tsx")):
        header_to_apply = TS_HEADER
        lines_to_remove = 4
    elif file_path.endswith(".go"):
        header_to_apply = GO_HEADER
        lines_to_remove = 2
    else:
        return # Not a file we need to process

    print(f"UPDATING: {file_path}")

    try:
        # Read the original content first
        with open(file_path, 'r', encoding='utf-8') as f:
            original_lines = f.readlines()

        # Overwrite the file with the new content
        with open(file_path, 'w', encoding='utf-8') as f:
            f.write(header_to_apply)
            
            # Get the remaining content after removing header lines
            remaining_lines = original_lines[lines_to_remove:]
            
            # For TS/TSX files, ensure exactly one blank line after header
            if file_path.endswith((".ts", ".tsx")):
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