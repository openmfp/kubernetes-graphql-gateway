# Handling Dotted Keys in GraphQL

This document explains how to work with Kubernetes labels, annotations, and other fields that contain dots in their keys (e.g., `app.kubernetes.io/name`) in GraphQL queries and mutations.

## Problem

GraphQL doesn't support dots in field names, but Kubernetes extensively uses dotted keys in:
- `metadata.labels`
- `metadata.annotations` 
- `spec.nodeSelector`
- `spec.selector.matchLabels`

## Solution: StringMapInput Scalar

The gateway uses a custom `StringMapInput` scalar that accepts arrays of `{key, value}` objects for input and returns direct maps for output.

