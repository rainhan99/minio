// This file is part of MinIO Design System
// Copyright (c) 2023 MinIO, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

import * as React from "react";
import { SVGProps } from "react";

const LockFilledIcon = (props: SVGProps<SVGSVGElement>) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    className={`min-icon`}
    fill={"currentcolor"}
    viewBox="0 0 256 256"
    {...props}
  >
    <path d="m209.35,76.15h-28.21v-26.24c-1.65-27.75-25.38-48.97-53.14-47.55-27.76-1.43-51.48,19.8-53.14,47.55v26.24h-28.21c-23.19-1.15-42.98,16.61-44.32,39.8v83.57c1.26,22.76,20.4,40.4,43.19,39.8l82.48,14.39,82.48-14.41c22.78.6,41.92-17.02,43.19-39.77v-83.57c-1.34-23.18-21.13-40.95-44.32-39.8m-70.88,86.61v15.16c0,5.47-4.42,9.9-9.89,9.93h-1.19c-5.47-.03-9.88-4.48-9.87-9.95v-15.14c-5.16-3.51-8.25-9.34-8.25-15.58h0c.08-10.34,8.53-18.66,18.87-18.58,10.34.08,18.66,8.53,18.58,18.87-.05,6.12-3.08,11.83-8.13,15.29m20.63-86.61h-62.35v-26.24c.97-16.26,14.86-28.69,31.12-27.86,16.26-.83,30.16,11.6,31.12,27.86l.1,26.24Z" />
  </svg>
);

export default LockFilledIcon;
