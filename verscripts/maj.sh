#!/bin/sh
maj=$(git show-ref --tags | tail -n1| cut -d" " -f2| cut -d/ -f3)
echo ${maj:-0}
