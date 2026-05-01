package llm

// Hand-crafted templates that the mock LLM "generates". Each has:
//   - a problem stem with verifiable answer
//   - a FigureSpec (JSON) for SVG rendering (geometry only)
//   - distractors tagged with specific misconceptions

func geomTemplates() []geomTemplate {
	return []geomTemplate{
		{
			id:    "flagpole-shadow",
			skill: "trig.tan",
			stemFmt: "A flagpole casts a shadow 24 m long on level ground. " +
				"The angle of elevation from the tip of the shadow to the top of " +
				"the flagpole is 35°. How tall is the flagpole, to the nearest metre?",
			answer:     "16.8",
			numeric:    16.8,
			tolerance:  0.5,
			steps:      []string{"Identify opposite (h) and adjacent (24).", "Use tan(35°) = h/24.", "h = 24 · tan(35°) ≈ 16.8."},
			explanation: "tan(35°) = h / 24, so h = 24 · tan(35°) ≈ 16.8 m.",
			figureSpec: `{
				"width": 360, "height": 240, "view_box": "0 0 360 240",
				"primitives": [
					{"kind":"line","attrs":{"x1":20,"y1":200,"x2":340,"y2":200},"style":"default"},
					{"kind":"line","attrs":{"x1":280,"y1":200,"x2":280,"y2":60},"style":"default","id":"pole"},
					{"kind":"line","attrs":{"x1":40,"y1":200,"x2":280,"y2":200},"style":"highlight","id":"shadow"},
					{"kind":"line","attrs":{"x1":40,"y1":200,"x2":280,"y2":60},"style":"dashed"},
					{"kind":"right_angle","attrs":{"x":280,"y":200,"size":10}},
					{"kind":"angle_marker","attrs":{"cx":40,"cy":200,"r":35,"start":0,"sweep":-35}}
				],
				"labels": [
					{"text":"35°","x":80,"y":195,"style":"highlight"},
					{"text":"24 m","x":150,"y":220,"style":"default"},
					{"text":"h = ?","x":295,"y":135,"style":"default"}
				]
			}`,
			choices: `[
				{"text":"13.8 m","is_correct":false,"misconception":"Used cos instead of tan — cos gives adjacent over hypotenuse, but here we have adjacent and need opposite."},
				{"text":"16.8 m","is_correct":true,"rationale":"tan(35°) = h / 24, so h = 24 · tan(35°) ≈ 16.8 m."},
				{"text":"19.7 m","is_correct":false,"misconception":"Used sin instead of tan — sin needs the hypotenuse, which isn't given."},
				{"text":"34.3 m","is_correct":false,"misconception":"Divided instead of multiplied — h = 24 / tan(35°) gives the wrong side."}
			]`,
		},
		{
			id:    "inscribed-angle",
			skill: "circles.inscribed_angle",
			stemFmt: "Points A, B, C lie on a circle with centre O. " +
				"The central angle ∠AOC measures 110°. What is the measure of " +
				"the inscribed angle ∠ABC?",
			answer:     "55",
			numeric:    55,
			tolerance:  0.1,
			steps:      []string{"Inscribed angle = ½ × central angle on the same arc.", "110° / 2 = 55°."},
			explanation: "The inscribed angle theorem: ∠ABC is half the central angle subtending the same arc, so 110°/2 = 55°.",
			figureSpec: `{
				"width": 320, "height": 280, "view_box": "0 0 320 280",
				"primitives": [
					{"kind":"circle","attrs":{"cx":160,"cy":140,"r":100},"style":"default"},
					{"kind":"circle","attrs":{"cx":160,"cy":140,"r":3},"style":"soft_fill"},
					{"kind":"circle","attrs":{"cx":78.1,"cy":82.6,"r":4},"style":"soft_fill"},
					{"kind":"circle","attrs":{"cx":241.9,"cy":82.6,"r":4},"style":"soft_fill"},
					{"kind":"circle","attrs":{"cx":160,"cy":240,"r":4},"style":"soft_fill"},
					{"kind":"line","attrs":{"x1":160,"y1":140,"x2":78.1,"y2":82.6},"style":"highlight"},
					{"kind":"line","attrs":{"x1":160,"y1":140,"x2":241.9,"y2":82.6},"style":"highlight"},
					{"kind":"line","attrs":{"x1":160,"y1":240,"x2":78.1,"y2":82.6},"style":"default"},
					{"kind":"line","attrs":{"x1":160,"y1":240,"x2":241.9,"y2":82.6},"style":"default"}
				],
				"labels": [
					{"text":"O","x":170,"y":138,"style":"default"},
					{"text":"A","x":62,"y":76,"style":"default"},
					{"text":"C","x":248,"y":76,"style":"default"},
					{"text":"B","x":155,"y":258,"style":"default"},
					{"text":"110°","x":160,"y":118,"style":"highlight"},
					{"text":"?","x":160,"y":218,"style":"default"}
				]
			}`,
			choices: `[
				{"text":"35°","is_correct":false,"misconception":"Halved twice — only one halving is needed by the inscribed angle theorem."},
				{"text":"55°","is_correct":true,"rationale":"By the inscribed angle theorem, ∠ABC = 110° / 2 = 55°."},
				{"text":"70°","is_correct":false,"misconception":"Computed (180° − 110°) / 2 — that applies for a different relationship."},
				{"text":"110°","is_correct":false,"misconception":"Used the central angle directly — the inscribed angle is half of it."}
			]`,
		},
		{
			id:    "similar-triangles",
			skill: "similar_triangles.ratio",
			stemFmt: "Triangle ABC is similar to triangle DEF. " +
				"If AB = 6, BC = 8, and DE = 9, find the length of EF.",
			answer:     "12",
			numeric:    12,
			tolerance:  0.01,
			steps:      []string{"Scale factor = DE/AB = 9/6 = 1.5.", "EF = BC × scale factor = 8 × 1.5 = 12."},
			explanation: "The scale factor from ABC to DEF is 9/6 = 1.5. So EF = 8 × 1.5 = 12.",
			figureSpec: `{
				"width": 380, "height": 240, "view_box": "0 0 380 240",
				"primitives": [
					{"kind":"polygon","attrs":{"points_count":3,"x0":40,"y0":200,"x1":40,"y1":140,"x2":110,"y2":200},"style":"soft_fill"},
					{"kind":"right_angle","attrs":{"x":40,"y":200,"size":8}},
					{"kind":"polygon","attrs":{"points_count":3,"x0":240,"y0":210,"x1":240,"y1":120,"x2":345,"y2":210},"style":"highlight"},
					{"kind":"right_angle","attrs":{"x":240,"y":210,"size":10}}
				],
				"labels": [
					{"text":"B","x":32,"y":215,"style":"default"},
					{"text":"A","x":32,"y":135,"style":"default"},
					{"text":"C","x":115,"y":215,"style":"default"},
					{"text":"6","x":20,"y":175,"style":"default"},
					{"text":"8","x":70,"y":218,"style":"default"},
					{"text":"∼","x":180,"y":180,"style":"mono"},
					{"text":"E","x":232,"y":225,"style":"default"},
					{"text":"D","x":232,"y":115,"style":"default"},
					{"text":"F","x":350,"y":225,"style":"default"},
					{"text":"9","x":218,"y":170,"style":"default"},
					{"text":"?","x":290,"y":228,"style":"highlight"}
				]
			}`,
			choices: `[
				{"text":"10.5","is_correct":false,"misconception":"Added 1.5 instead of multiplying — scale factor multiplies, doesn't add."},
				{"text":"11","is_correct":false,"misconception":"Computed 8 + (9 − 6) — similar triangles preserve ratios, not differences."},
				{"text":"12","is_correct":true,"rationale":"Scale factor = 9/6 = 1.5, so EF = 8 × 1.5 = 12."},
				{"text":"13.5","is_correct":false,"misconception":"Applied the scale factor to the wrong side."}
			]`,
		},
	}
}

func algTemplates() []algTemplate {
	return []algTemplate{
		{
			id:    "linear-eq-1",
			skill: "algebra.linear",
			stem:  "Solve for x: 3x − 7 = 2x + 5.",
			answer: "12",
			numeric: 12, tolerance: 0.001,
			steps:      []string{"Subtract 2x: x − 7 = 5.", "Add 7: x = 12."},
			explanation: "Move x-terms to one side: x = 12.",
			choices: `[
				{"text":"x = 5","is_correct":false,"misconception":"Subtracted instead of adding 7."},
				{"text":"x = -12","is_correct":false,"misconception":"Sign error when isolating x."},
				{"text":"x = 12","is_correct":true,"rationale":"3x − 2x = 5 + 7 ⟹ x = 12."},
				{"text":"x = 2.4","is_correct":false,"misconception":"Divided rather than subtracting like terms."}
			]`,
		},
		{
			id:    "quadratic-1",
			skill: "algebra.quadratic",
			stem:  "Find the positive root of x² − 5x − 14 = 0.",
			answer: "7",
			numeric: 7, tolerance: 0.001,
			steps:      []string{"Factor: (x − 7)(x + 2) = 0.", "Roots: x = 7 or x = −2.", "Positive root: x = 7."},
			explanation: "Factoring: (x − 7)(x + 2) = 0 gives x = 7 or x = −2; the positive root is 7.",
			choices: `[
				{"text":"x = 2","is_correct":false,"misconception":"Picked the wrong factor — confused the signs in factoring."},
				{"text":"x = -7","is_correct":false,"misconception":"Sign error — the positive root was asked for."},
				{"text":"x = 14","is_correct":false,"misconception":"Used the constant term directly without factoring."},
				{"text":"x = 7","is_correct":true,"rationale":"(x − 7)(x + 2) = 0; the positive root is x = 7."}
			]`,
		},
		{
			id:    "system-1",
			skill: "algebra.systems",
			stem:  "Solve the system: 2x + y = 11 and x − y = 1.",
			answer: "x = 4, y = 3",
			numeric: 4, tolerance: 0.001,
			steps:      []string{"Add equations: 3x = 12.", "x = 4.", "Substitute: y = 3."},
			explanation: "Adding the equations gives 3x = 12 so x = 4, then y = x − 1 = 3.",
			choices: `[
				{"text":"x = 4, y = 3","is_correct":true,"rationale":"Adding eliminates y: 3x = 12 ⟹ x = 4, then y = 3."},
				{"text":"x = 3, y = 4","is_correct":false,"misconception":"Swapped x and y values."},
				{"text":"x = 5, y = 1","is_correct":false,"misconception":"Subtracted instead of adding the equations."},
				{"text":"x = 4, y = -3","is_correct":false,"misconception":"Sign error when back-substituting."}
			]`,
		},
	}
}
