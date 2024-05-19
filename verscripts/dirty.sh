#!/bin/bash
state="clean"
git status --untracked-files=no --porcelain|  grep -q M && hash=$(git diff | md5sum | cut -d ' ' -f1) && state="dirty-${hash:0:5}"
echo $state
