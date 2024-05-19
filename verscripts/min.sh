#!/bin/bash
if ! git show-ref --tags >/dev/null 2>&1; then
	num_commits=$(git rev-list --count HEAD)
else
	first_tag=$(git show-ref --tags | tail -n1| cut -d" " -f1)
	last_commit=$(git log --pretty=oneline | head -n1| cut -d" " -f1)
	num_commits=$(git rev-list --count $first_tag..$last_commit)
fi
echo "${num_commits:-0}"


