set -euo pipefail
script_dirpath="$(cd "$(dirname "${0}")" && pwd)"
root_dirpath="$(dirname "${script_dirpath}")"

# ==========================================================================================
#                                         Constants
# ==========================================================================================
BOOTSTRAP_SCRIPTS_DIRNAME="bootstrap"
BOOTSTRAP_SCRIPT_FILENAME="bootstrap.sh"
SUPPORTED_LANGS_FILENAME="supported-languages.txt"
SCRIPTS_DIRNAME_INSIDE_TESTSUITE="scripts"
BUILD_AND_RUN_FILENAME="build-and-run.sh"
BUILD_AND_RUN_ALL_CMD="all"

GIT_USER_EMAIL_PROPERTY="user.email"
GIT_USER_NAME_PROPERTY="user.name"

# Bootstrapping normally requires input from STDIN, but we can set
#  certain variables so this isn't required for CI
# NOTE: This won't handle flag values that contain spaces, though it can handle multiple flags separated by a space
declare -A CUSTOM_LANG_BOOTSTRAP_FLAGS 
CUSTOM_LANG_BOOTSTRAP_FLAGS[golang]="GO_NEW_MODULE_NAME=github.com/test/test-module"



# ==========================================================================================
#                                        Arg-parsing
# ==========================================================================================
docker_username="${1:-}"
docker_password_DO_NOT_LOG="${2:-}" # WARNING: DO NOT EVER LOG THIS!!
kurtosis_client_id="${3:-}"
kurtosis_client_secret_DO_NOT_LOG="${4:-}" # WARNING: DO NOT EVER LOG THIS!!

# ==========================================================================================
#                                        Arg validation
# ==========================================================================================
if [ -z "${docker_username}" ]; then
    echo "Error: Docker username cannot be empty" >&2
    exit 1
fi
if [ -z "${docker_password_DO_NOT_LOG}" ]; then
    echo "Error: Docker password cannot be empty" >&2
    exit 1
fi
if [ -z "${kurtosis_client_id}" ]; then
    echo "Error: Kurtosis client ID cannot be empty" >&2
    exit 1
fi
if [ -z "${kurtosis_client_secret_DO_NOT_LOG}" ]; then
    echo "Error: Kurtosis client secret cannot be empty" >&2
    exit 1
fi


# ==========================================================================================
#                                           Main code
# ==========================================================================================
# Docker is restricting anonymous image pulls, so we log in before we do any pulling
if ! docker login -u "${docker_username}" -p "${docker_password_DO_NOT_LOG}"; then
    echo "Error: Logging in to Docker failed" >&2
    exit 1
fi

# Git needs to be initialized, since the bootstrap will create a new Git repo and commit to it
if ! { git config --list | grep "${GIT_USER_EMAIL_PROPERTY}"; } || ! { git config --list | grep "${GIT_USER_NAME_PROPERTY}"; }; then
    if ! git config --global "${GIT_USER_EMAIL_PROPERTY}" "bootstrap-tester@test.com"; then
        echo "Error: An error occurred configuring the Git user email property '${GIT_USER_EMAIL_PROPERTY}'" >&2
        exit 1
    fi
    if ! git config --global "${GIT_USER_NAME_PROPERTY}" "Bootstrap Tester"; then
        echo "Error: An error occurred configuring the Git user name propery '${GIT_USER_NAME_PROPERTY}'" >&2
        exit 1
    fi
fi

bootstrap_script_filepath="${root_dirpath}/${BOOTSTRAP_SCRIPTS_DIRNAME}/${BOOTSTRAP_SCRIPT_FILENAME}"
echo "Bootstrapping and running new testsuites for all languages..."
for lang in $(cat "${root_dirpath}/${SUPPORTED_LANGS_FILENAME}"); do
    echo "Bootstrapping and running ${lang} testsuite..."
    output_dirpath="$(mktemp -d)"
    testsuite_image="bootstrap-test-${lang}-image"
    lang_specific_vars_to_set="${CUSTOM_LANG_BOOTSTRAP_FLAGS[${lang}]}"
    command="${lang_specific_vars_to_set} ${bootstrap_script_filepath} ${lang} ${output_dirpath} ${testsuite_image}"
    if ! eval "${command}"; then
        echo "Error: Bootstrapping ${lang} testsuite failed" >&2
        exit 1
    fi

    build_and_run_filepath="${output_dirpath}/${SCRIPTS_DIRNAME_INSIDE_TESTSUITE}/${BUILD_AND_RUN_FILENAME}"
    if ! "${build_and_run_filepath}" "${BUILD_AND_RUN_ALL_CMD}" --client-id "${kurtosis_client_id}" --client-secret "${kurtosis_client_secret_DO_NOT_LOG}"; then
        echo "Error: The bootstrapped ${lang} testsuite failed" >&2
        exit 1
    fi
    echo "Successfully bootstrapped and ran new ${lang} testsuite"
done
echo "Successfully bootstrapped and ran new testsuites for all languages!"
