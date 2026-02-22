#!/usr/bin/env python

import sys

import argparse
import os
import yaml


def indent(s, level):
    istr = " " * level

    return "\n".join([ f"{istr}{line}" for line in s.splitlines() ])


def _filter_d6e_union(level:int, spec_name:str, spec_dict:dict) -> bool:
    """Recursively spec-type dictionaries to handle d6e types."""

    # istr = " " * (level * 2)
    # text = yaml.safe_dump(spec_dict)

    # print(f"{istr}==== {spec_name}")
    # for line in text.splitlines():
    #     print(f"{istr}{line}")

    # print(f"{istr}==== {spec_name} type: {spec_dict.get('type')}")

    # If this is a d6e- type, return True so that the caller can delete it
    # from the parent.

    if spec_dict.get("type", "").startswith("d6e"):
        # print(f"{istr}  d6e- type")
        return True

    # Weird shortcut. If we have "items" with a d6e- type, we can just
    # short-circuit.
    items = spec_dict.get("items")
    if isinstance(items, dict):
        if items.get("type", "").startswith("d6e"):
            # print(f"{istr}  d6e- type in items")
            return True

    # So this is not a d6e- type itself. Iterate through its properties
    # and handle all of them.

    d6e_elements = []
    altered = False

    properties = spec_dict.get("properties", {})
    for name, defn in properties.items():
        if isinstance(defn, dict):
            # Check the type of this defn itself.
            if _filter_d6e_union(level + 1, name, defn):
                d6e_elements.append(name)

    # Remove d6e type properties and mark parent for unknown field preservation
    if d6e_elements:
        # print(f"{istr}==== {spec_name} removing d6e types: {d6e_elements}")
        # print(f"{istr}  keys before removal: {list(properties.keys())}")

        for d6e_name in d6e_elements:
            # print(f"{istr}  drop {d6e_name}")
            del properties[d6e_name]

        # print(f"{istr}  set preserve-unknown-fields")
        spec_dict["x-kubernetes-preserve-unknown-fields"] = True
        altered = True

    # Finally, check additionalProperties and items
    for key in ["additionalProperties", "items"]:
        check_dict = spec_dict.get(key)
        if isinstance(check_dict, dict):
            if _filter_d6e_union(level + 1, key, check_dict):
                # print(f"{istr}  drop {key}")
                del spec_dict[key]
                # print(f"{istr}  set preserve-unknown-fields")
                spec_dict["x-kubernetes-preserve-unknown-fields"] = True
                altered = True

    # if altered:
    #     text = yaml.safe_dump(spec_dict)

    #     print(f"{istr}==== {spec_name} after filtering d6e types")

    #     for line in text.splitlines():
    #         print(f"{istr}{line}")

    return False


def filter_d6e_union(name, version: dict) -> dict:
    """Remove d6e.io/union annotations from a version dict."""

    # Dump the original yaml to /tmp/original-kind.yaml for debugging
    with open(f"/tmp/original-{name}-{version['name']}.yaml", "w") as debug_file:
        yaml.safe_dump(version, debug_file)

    # If we somehow don't have a real spec with properties defined, this
    # will raise an exception. That's intentional.

    spec = version['schema']['openAPIV3Schema']['properties']['spec']

    # Recursively filter d6e union types
    _filter_d6e_union(0, f"{version['name']}-spec", spec)

    return version


def main():
    parser = argparse.ArgumentParser(
        description="Split CRD YAML files into individual Helm chart templates"
    )
    parser.add_argument(
        "--output", "-o",
        required=True,
        help="Output directory for the split CRD files"
    )
    parser.add_argument(
        "crd_files",
        nargs="+",
        help="One or more paths to CRD YAML files to process"
    )

    args = parser.parse_args()

    # Create output directory if it doesn't exist
    os.makedirs(args.output, exist_ok=True)

    for path in args.crd_files:
        crd = yaml.safe_load(open(path))

        # Make sure it's really a CRD.
        if crd['kind'] != 'CustomResourceDefinition':
            continue

        # Grab the name of the CRD.
        name = crd['metadata']['name']
        shortname = name.replace(".getambassador.io", "")

        versions = {}

        for version in crd['spec']['versions']:
            versions[version['name']] = version

        output_path = os.path.join(args.output, f"{shortname}.yaml")
        output = open(output_path, 'w')

        print(f"{name}: {output_path}")

        metadata = crd['metadata']

        if "labels" not in metadata:
            metadata["labels"] = {}

        # Force the app.kubernetes.io/part-of label.
        metadata["labels"]["app.kubernetes.io/part-of"] = "emissary-ingress"

        if "helm.sh/chart" not in metadata["labels"]:
            metadata["labels"]["helm.sh/chart"] = "XXXLABEL-HELM-CHARTXXX"

        if "emissary-ingress.dev/control-plane-ns" not in metadata["labels"]:
            metadata["labels"]["emissary-ingress.dev/control-plane-ns"] = "XXXLABEL-CONTROL-PLANE-NSXXX"

        info = {
            'apiVersion': crd['apiVersion'],
            'kind': crd['kind'],
            'metadata': metadata,
            }

        s = yaml.safe_dump(info)

        s = s.replace("XXXLABEL-HELM-CHARTXXX", '{{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}')
        s = s.replace("XXXLABEL-CONTROL-PLANE-NSXXX", '{{ .Release.Namespace }}')

        output.write(s)
        output.write(f"spec:\n")

        if "v1" in versions or "v2" in versions:
            output.write('  {{- include "partials.conversion" . }}\n')

        n = crd["spec"]["names"]
        categories = n.get('categories')

        if categories:
            n['categories'] = categories
        else:
            n['categories'] = [ "ambassador-crds" ]

        info = {
            "group": crd['spec']['group'],
            "names": crd['spec']['names'],
            "scope": crd['spec']['scope'],
            "preserveUnknownFields": crd['spec'].get('preserveUnknownFields', False),
        }

        output.write(indent(yaml.safe_dump(info), 2))
        output.write("\n")

        output.write("  versions:\n")

        if "v1" in versions or "v2" in versions:
            output.write("{{- if .Values.enableLegacyVersions }}\n")

        if "v1" in versions:
            v1 = filter_d6e_union(name, versions["v1"])

            output.write("{{- if .Values.enableV1 }}\n")
            output.write(indent(yaml.safe_dump([ v1 ]), 2))
            output.write("\n")
            output.write("{{- end }}\n")

        if "v2" in versions:
            v2 = filter_d6e_union(name, versions["v2"])

            output.write(indent(yaml.safe_dump([ v2 ]), 2))
            output.write("\n")
            output.write("{{- end }}\n")

        if "v3alpha1" in versions:
            v = versions["v3alpha1"]
            v["storage"] = "XXXSTORAGEXXX"
            s = indent(yaml.safe_dump([ v ]), 2)

            if ("v1" in versions or "v2" in versions):
                s = s.replace("XXXSTORAGEXXX", '{{ include "partials.v3alpha1storage" . }}')
            else:
                s = s.replace("XXXSTORAGEXXX", 'true')

            output.write(s)
            output.write("\n")

        output.close()


if __name__ == "__main__":
    main()

