#!/usr/bin/env bash
cd cx-api && bash build_facade_files.sh
cd ..

if diff <(jq --sort-keys . apidocs_golden.swagger.json | jq 'walk(if type == "object" and has("oneOf") and (.oneOf | type) == "array" then .oneOf |= sort_by(."$ref") else . end)') \
     <(jq --sort-keys . apidocs.swagger.json | jq 'walk(if type == "object" and has("oneOf") and (.oneOf | type) == "array" then .oneOf |= sort_by(."$ref") else . end)') > /dev/null; then
  echo "Plugin output matches golden file without preview visibility."
else
  echo "Plugin output does not match golden file without preview visibility."
  exit 1
fi

rm -r apidocs.swagger.json

cd cx-api && bash build_facade_files_with_preview_visibility.sh
cd ..

if diff <(jq --sort-keys . apidocs_golden_preview_visibility.swagger.json | jq 'walk(if type == "object" and has("oneOf") and (.oneOf | type) == "array" then .oneOf |= sort_by(."$ref") else . end)') \
     <(jq --sort-keys . apidocs.swagger.json | jq 'walk(if type == "object" and has("oneOf") and (.oneOf | type) == "array" then .oneOf |= sort_by(."$ref") else . end)') > /dev/null; then
  echo "Plugin output matches golden file with preview visibility."
else
  echo "Plugin output does not match golden file with preview visibility."
  exit 1
fi

rm -r apidocs.swagger.json