set -euo pipefail

# =============================================================================
#                                    Constants
# =============================================================================
TESTSUITE_IMPL_DIRNAME="testsuite"

# Constants 
GO_MOD_FILENAME="go.mod"
GO_MOD_MODULE_KEYWORD="module "  # The key we'll look for when replacing the module name in go.mod

KURTOSIS_LIB_MODULE="github.com/kurtosis-tech/kurtosis-libs/golang"

# =============================================================================
#                             Arg-Parsing & Validation
# =============================================================================
input_dirpath="${1:-}"
if [ -z "${input_dirpath}" ]; then
    echo "Error: Empty source directory to copy from" >&2
    exit 1
fi
if ! [ -d "${input_dirpath}" ]; then
    echo "Error: Dirpath to copy source from '${input_dirpath}' is nonexistent" >&2
    exit 1
fi

output_dirpath="${2:-}"
if [ -z "${output_dirpath}" ]; then
    echo "Error: Empty output directory to copy to" >&2
    exit 1
fi
if ! [ -d "${output_dirpath}" ]; then
    echo "Error: Output dirpath to copy to '${output_dirpath}' is nonexistent" >&2
    exit 1
fi


# =============================================================================
#                               Copying Files
# =============================================================================
cp "${input_dirpath}/.dockerignore" "${output_dirpath}/"
cp "${input_dirpath}/Dockerfile" "${output_dirpath}/"
cp "${input_dirpath}/${GO_MOD_FILENAME}" "${output_dirpath}/"
cp "${input_dirpath}/go.sum" "${output_dirpath}/"
cp -r "${input_dirpath}/${TESTSUITE_IMPL_DIRNAME}" "${output_dirpath}/"


# =============================================================================
#                         Post-Copy Modifications
# =============================================================================
new_module_name=""
while [ -z "${new_module_name}" ]; do
    read -p "Name for the Go module that will contain your testsuite project (e.g. github.com/my-org/my-repo): " new_module_name
done

# Validation, to save us in case someone changes stuff in the future
go_mod_filepath="${output_dirpath}/${GO_MOD_FILENAME}"
if [ "$(grep "^${GO_MOD_MODULE_KEYWORD}" "${go_mod_filepath}" | wc -l)" -ne 1 ]; then
    echo "Validation failed: Could not find exactly one line in ${GO_MOD_FILENAME} with keyword '${GO_MOD_MODULE_KEYWORD}' for use when replacing with the user's module name" >&2
    exit 1
fi

# Replace module names in code (we need the "-i '' " argument because Mac sed requires it)
existing_module_name="$(grep "module" "${go_mod_filepath}" | awk '{print $2}')"
if ! sed -i '' "s,${existing_module_name},${new_module_name},g" ${go_mod_filepath}; then
    echo "Error: Could not replace Go module name in mod file '${go_mod_filepath}'" >&2
    exit 1
fi
# We search for old_module_name/testsuite because we don't want the old_module_name/lib entries to get renamed
if ! sed -i '' "s,${existing_module_name}/${TESTSUITE_IMPL_DIRNAME},${new_module_name}/${TESTSUITE_IMPL_DIRNAME},g" $(find "${output_dirpath}" -type f); then
    echo "Error: Could not replace Go module name in code files" >&2
    exit 1
fi

# Lastly, depend on the actual Kurtosis library
if ! ( cd "${output_dirpath}" && go get "${KURTOSIS_LIB_MODULE}" ); then
    echo "Error: Failed to pull Kurtosis Go lib dependency '${KURTOSIS_LIB_MODULE}'" >&2
    exit 1
fi