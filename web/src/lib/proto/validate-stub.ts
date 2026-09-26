import type { GenFile } from "@bufbuild/protobuf/codegenv2";
import { fileDesc } from "@bufbuild/protobuf/codegenv2";

/**
 * Stand-in for the generated buf/validate/validate_pb.ts (vite.config.ts
 * aliases it). The API descriptors import validate.proto only for field
 * options, which the UI never evaluates: an empty file of the same name
 * satisfies the import and keeps ~230 KB of descriptors out of the bundle.
 */
export const file_buf_validate_validate: GenFile = /*@__PURE__*/ fileDesc(
  "ChtidWYvdmFsaWRhdGUvdmFsaWRhdGUucHJvdG8SDGJ1Zi52YWxpZGF0ZWIGcHJvdG8y",
);
