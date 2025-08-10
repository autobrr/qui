#!/bin/bash

# Script to update copyright headers in TypeScript, TSX, and Go files
# Also adds headers to files missing them completely
# Usage: ./update_copyright_headers.sh [--add-missing|-a]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counter variables
ts_files_updated=0
ts_files_added=0
go_files_updated=0
go_files_added=0
ts_files_checked=0
go_files_checked=0

# New header for TypeScript/TSX files
NEW_TS_HEADER="/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */"

# New header for Go files (using line comments)
NEW_GO_HEADER="// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT"

echo -e "${GREEN}Starting copyright header update...${NC}"
echo -e "${BLUE}TypeScript/TSX: s0up and the autobrr contributors${NC}"
echo -e "${BLUE}Go files: s0up and the autobrr contributors${NC}"
echo ""

# Function to check if file has the old GPL header
has_old_header() {
    local file="$1"
    # Check first 10 lines for GPL license or incorrect copyright years
    if head -n 10 "$file" | grep -q "GPL-2.0-or-later"; then
        return 0
    fi
    # Also check for 2024-2025 copyright that needs updating
    if head -n 10 "$file" | grep -q "Copyright.*2024-2025"; then
        return 0
    fi
    # Check for outdated copyright in Go files
    if [[ "$file" == *.go ]] && head -n 10 "$file" | grep -q "Ludvig Lundgren and the autobrr"; then
        return 0
    fi
    return 1
}

# Function to check if file has any copyright header
has_any_header() {
    local file="$1"
    # Check first 10 lines for any copyright or license identifier
    head -n 10 "$file" | grep -qi "copyright\|spdx-license-identifier"
}

# Function to add header to file without one
add_header_to_file() {
    local file="$1"
    local header="$2"
    local temp_file=$(mktemp)
    
    # For Go files, use line comments format
    if [[ "$file" == *.go ]]; then
        echo "// Copyright (c) 2025, s0up and the autobrr contributors." > "$temp_file"
        echo "// SPDX-License-Identifier: MIT" >> "$temp_file"
        echo "" >> "$temp_file"
        cat "$file" >> "$temp_file"
    else
        # For TS/TSX files, use block comment format
        echo "$header" > "$temp_file"
        echo "" >> "$temp_file"
        cat "$file" >> "$temp_file"
    fi
    
    mv "$temp_file" "$file"
}

# Function to update TypeScript and TSX files
update_ts_files() {
    echo -e "${YELLOW}Processing TypeScript and TSX files...${NC}"
    
    # Find all .ts and .tsx files
    while IFS= read -r -d '' file; do
        ((ts_files_checked++))
        
        # Check if file has old GPL header that needs updating
        if has_old_header "$file"; then
            # Create a temporary file
            temp_file=$(mktemp)
            
            # Use awk to replace the header
            awk '
            BEGIN { 
                in_header = 0
                header_end = 0
                new_header = "/*\n * Copyright (c) 2025, s0up and the autobrr contributors.\n * SPDX-License-Identifier: MIT\n */"
                printed_new = 0
            }
            {
                # Detect start of old header
                if (!header_end && /^\/\*/) {
                    in_header = 1
                }
                
                # If we are in the header and find GPL license
                if (in_header && /GPL-2\.0-or-later/) {
                    # Skip the rest of the old header
                    while ((getline) > 0 && !/\*\//) {
                        # Skip lines until end of comment
                    }
                    # Print new header
                    if (!printed_new) {
                        print new_header
                        printed_new = 1
                    }
                    header_end = 1
                    next
                }
                
                # If we are in header but no GPL found, just print
                if (in_header && /\*\//) {
                    in_header = 0
                    header_end = 1
                }
                
                # Print all other lines
                if (!in_header || header_end) {
                    print
                }
            }
            ' "$file" > "$temp_file"
            
            # Check if the file was actually changed
            if ! cmp -s "$file" "$temp_file"; then
                mv "$temp_file" "$file"
                echo -e "  ${GREEN}✓${NC} Updated: $file"
                ((ts_files_updated++))
            else
                rm "$temp_file"
            fi
        # Check if file has no header at all
        elif ! has_any_header "$file"; then
            add_header_to_file "$file" "$NEW_TS_HEADER"
            echo -e "  ${BLUE}+${NC} Added header to: $file"
            ((ts_files_added++))
        fi
    done < <(find . -type f \( -name "*.ts" -o -name "*.tsx" \) -not -path "*/node_modules/*" -not -path "*/.git/*" -not -path "*/dist/*" -not -path "*/build/*" -print0)
}

# Function to update Go files
update_go_files() {
    echo -e "${YELLOW}Processing Go files...${NC}"
    
    # Find all .go files
    while IFS= read -r -d '' file; do
        ((go_files_checked++))
        
        # Check if file has old GPL header that needs updating
        if has_old_header "$file"; then
            # Create a temporary file
            temp_file=$(mktemp)
            
            # Check if it's block comment or line comment style
            if head -n 1 "$file" | grep -q "^/\*"; then
                # Block comment style - replace with line comment style
                awk '
                BEGIN { 
                    in_header = 0
                    header_end = 0
                    new_header_1 = "// Copyright (c) 2025, s0up and the autobrr contributors."
                    new_header_2 = "// SPDX-License-Identifier: MIT"
                    printed_new = 0
                }
                {
                    # Detect start of old header
                    if (!header_end && /^\/\*/) {
                        in_header = 1
                    }
                    
                    # If we are in the header and find copyright or license info
                    if (in_header && (/GPL-2\.0-or-later/ || /Ludvig/ || /2024-2025/ || /2021 - 2025/ || /Copyright/)) {
                        # Skip the rest of the old header
                        while ((getline) > 0 && !/\*\//) {
                            # Skip lines until end of comment
                        }
                        # Print new header with line comments
                        if (!printed_new) {
                            print new_header_1
                            print new_header_2
                            printed_new = 1
                        }
                        header_end = 1
                        next
                    }
                    
                    # If we are in header but no copyright found, just print
                    if (in_header && /\*\//) {
                        in_header = 0
                        header_end = 1
                    }
                    
                    # Print all other lines
                    if (!in_header || header_end) {
                        print
                    }
                }
                ' "$file" > "$temp_file"
            else
                # Line comment style
                awk '
                BEGIN { 
                    replaced = 0
                    new_header_1 = "// Copyright (c) 2025, s0up and the autobrr contributors."
                    new_header_2 = "// SPDX-License-Identifier: MIT"
                }
                {
                    # Replace old copyright line (various patterns)
                    if (!replaced && /^\/\/ Copyright/) {
                        if (/2024-2025/ || /2021 - 2025/ || /Ludvig/ || /GPL/) {
                            print new_header_1
                            replaced = 1
                            next
                        }
                    }
                    # Replace old license line
                    if (replaced == 1 && /^\/\/ SPDX-License-Identifier/) {
                        print new_header_2
                        replaced = 2
                        next
                    }
                    print
                }
                ' "$file" > "$temp_file"
            fi
            
            # Check if the file was actually changed
            if ! cmp -s "$file" "$temp_file"; then
                mv "$temp_file" "$file"
                echo -e "  ${GREEN}✓${NC} Updated: $file"
                ((go_files_updated++))
            else
                rm "$temp_file"
            fi
        # Check if file has no header at all
        elif ! has_any_header "$file"; then
            # Check if file starts with package declaration (typical for Go)
            if head -n 1 "$file" | grep -q "^package "; then
                # Add header before package declaration with line comments
                temp_file=$(mktemp)
                echo "// Copyright (c) 2025, s0up and the autobrr contributors." > "$temp_file"
                echo "// SPDX-License-Identifier: MIT" >> "$temp_file"
                echo "" >> "$temp_file"
                cat "$file" >> "$temp_file"
                mv "$temp_file" "$file"
            else
                # Just add header at the beginning
                temp_file=$(mktemp)
                echo "// Copyright (c) 2025, s0up and the autobrr contributors." > "$temp_file"
                echo "// SPDX-License-Identifier: MIT" >> "$temp_file"
                echo "" >> "$temp_file"
                cat "$file" >> "$temp_file"
                mv "$temp_file" "$file"
            fi
            echo -e "  ${BLUE}+${NC} Added header to: $file"
            ((go_files_added++))
        fi
    done < <(find . -type f -name "*.go" -not -path "*/vendor/*" -not -path "*/.git/*" -print0)
}

# Function to ask for confirmation
ask_confirmation() {
    local total_missing=$((ts_files_added + go_files_added))
    if [ $total_missing -gt 0 ]; then
        echo -e "${YELLOW}Found $total_missing files without headers.${NC}"
        echo -n "Do you want to add headers to files missing them? (y/N): "
        read -r response
        case "$response" in
            [yY][eE][sS]|[yY]) 
                return 0
                ;;
            *)
                return 1
                ;;
        esac
    fi
    return 0
}

# Main execution
main() {
    # Check if we're in a git repository (optional safety check)
    if [ -d .git ]; then
        echo -e "${GREEN}Git repository detected.${NC}"
        echo -e "${YELLOW}It's recommended to commit your changes before running this script.${NC}"
        echo ""
    fi
    
    # Parse command line arguments
    ADD_MISSING=false
    if [[ "${1:-}" == "--add-missing" ]] || [[ "${1:-}" == "-a" ]]; then
        ADD_MISSING=true
        echo -e "${BLUE}Will add headers to files missing them.${NC}"
        echo ""
    else
        echo -e "${BLUE}Run with --add-missing or -a to also add headers to files without any header.${NC}"
        echo ""
    fi
    
    # Temporarily store counts if we're not adding missing headers
    if [ "$ADD_MISSING" = false ]; then
        # First pass: just count and update existing headers
        ORIGINAL_ADD_MISSING=$ADD_MISSING
        ts_files_added=0
        go_files_added=0
    fi
    
    # Update TypeScript and TSX files
    update_ts_files
    echo ""
    
    # Update Go files
    update_go_files
    echo ""
    
    # Summary
    echo -e "${GREEN}=== Summary ===${NC}"
    echo -e "TypeScript/TSX files checked: $ts_files_checked"
    echo -e "TypeScript/TSX files with wrong license updated: ${GREEN}$ts_files_updated${NC}"
    if [ "$ADD_MISSING" = true ]; then
        echo -e "TypeScript/TSX files with missing headers fixed: ${BLUE}$ts_files_added${NC}"
    fi
    echo -e "Go files checked: $go_files_checked"
    echo -e "Go files with wrong license updated: ${GREEN}$go_files_updated${NC}"
    if [ "$ADD_MISSING" = true ]; then
        echo -e "Go files with missing headers fixed: ${BLUE}$go_files_added${NC}"
    fi
    
    total_updated=$((ts_files_updated + go_files_updated))
    total_added=$((ts_files_added + go_files_added))
    total_changed=$((total_updated + total_added))
    
    if [ $total_changed -gt 0 ]; then
        echo ""
        if [ $total_updated -gt 0 ]; then
            echo -e "${GREEN}Updated $total_updated file(s) with the correct MIT license.${NC}"
        fi
        if [ $total_added -gt 0 ]; then
            echo -e "${BLUE}Added headers to $total_added file(s) that were missing them.${NC}"
        fi
        echo -e "${YELLOW}Don't forget to review and commit the changes!${NC}"
    else
        echo ""
        if [ "$ADD_MISSING" = false ]; then
            # Count files missing headers for informational purposes
            missing_count=0
            while IFS= read -r -d '' file; do
                if ! has_any_header "$file"; then
                    ((missing_count++))
                fi
            done < <(find . -type f \( -name "*.ts" -o -name "*.tsx" -o -name "*.go" \) -not -path "*/node_modules/*" -not -path "*/.git/*" -not -path "*/vendor/*" -not -path "*/dist/*" -not -path "*/build/*" -print0)
            
            if [ $missing_count -gt 0 ]; then
                echo -e "${YELLOW}No files with wrong licenses found.${NC}"
                echo -e "${BLUE}Found $missing_count file(s) without any header. Run with --add-missing to add headers to them.${NC}"
            else
                echo -e "${GREEN}All files have correct headers!${NC}"
            fi
        else
            echo -e "${GREEN}All files have correct headers!${NC}"
        fi
    fi
}

# Run the main function with all arguments
main "$@"