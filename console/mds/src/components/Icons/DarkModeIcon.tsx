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

const DarkModeIcon = (props: SVGProps<SVGSVGElement>) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    className={"min-icon"}
    viewBox="0 0 256 256"
    {...props}
  >
    <g>
      <g transform="translate(0 0)">
        <path
          d="M228.4,151.3c-54.3,14-109.7-18.6-123.7-73c-4.3-16.6-4.3-34.1,0-50.7L111.8,0L84.9,9.4C34,27.3,0,75.3,0,129.2
			C0,199.1,56.9,256,126.8,256h0.1c53.9-0.1,101.8-34.1,119.6-84.9l9.4-26.9L228.4,151.3z"
          fill={"currentcolor"}
        />
      </g>
    </g>
  </svg>
);

export default DarkModeIcon;
