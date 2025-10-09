from PIL import Image, ImageDraw, ImageFont
import os
import re

# in/out
input_dir = "pics/mercator"
output_file = "out/mercator-map.jpg"

# regex hell
filename_pattern = re.compile(r"5-(\d+)-(\d+)\.jpg")

# parse filenames and sort images by position
image_positions = []
for filename in os.listdir(input_dir):
    match = filename_pattern.match(filename)
    if match:
        h_pos, v_pos = map(int, match.groups())
        image_positions.append((h_pos, v_pos, filename))

# sort by vert pos, then horizontal
image_positions.sort(key=lambda x: (x[1], x[0]))

images = {}
for h_pos, v_pos, filename in image_positions:
    img_path = os.path.join(input_dir, filename)
    images[(h_pos, v_pos)] = Image.open(img_path)

max_h = max(pos[0] for pos in images.keys()) + 1
max_v = max(pos[1] for pos in images.keys()) + 1

# calc size
img_width, img_height = next(iter(images.values())).size
total_width = max_h * img_width
total_height = max_v * img_height

# make canvas
stitched_image = Image.new("RGB", (total_width, total_height))


def create_placeholder_image(text, size):
    placeholder = Image.new("RGB", size, "black")
    draw = ImageDraw.Draw(placeholder)
    font = ImageFont.load_default()
    text_bbox = draw.textbbox((0, 0), text, font=font)
    text_width = text_bbox[2] - text_bbox[0]
    text_height = text_bbox[3] - text_bbox[1]
    text_x = (size[0] - text_width) // 2
    text_y = (size[1] - text_height) // 2
    draw.text((text_x, text_y), text, fill="white", font=font)
    return placeholder

# paste images onto the canvas based on their grid position
for h_pos in range(max_h):
    for v_pos in range(max_v):
        if (h_pos, v_pos) in images:
            img = images[(h_pos, v_pos)]
        else:
            filename = f"6-{h_pos}-{v_pos}.jpg"
            img = create_placeholder_image(filename, (img_width, img_height))
        x_offset = h_pos * img_width
        y_offset = v_pos * img_height
        stitched_image.paste(img, (x_offset, y_offset))

stitched_image.save(output_file)
print(f"Stitched image saved as {output_file}")
