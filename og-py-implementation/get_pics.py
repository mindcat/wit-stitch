import os
import wget
import subprocess
import re
import concurrent.futures
from threading import Lock

# Directory to save downloaded images
output_directory = "pics/mercator"
os.makedirs(output_directory, exist_ok=True)

def clean_url(url):
    pattern = r"(https://.*/TileGroup)\d+(/)\d+-\d+-\d+(\.jpg)"
    replacement = r"\1{}\2{}-{}-{}\3"
    return re.sub(pattern, replacement, url)

# Lock to prevent
grid_lock = Lock()

def display_grid(grid):
    """Clear the terminal and display the current grid."""
    os.system("clear")
    print("Downloading tiles...")
    for row in grid:
        print(" ".join(row))

def download_tile(url_og, i, j, max_tile, grid):
    """Download a single tile and update the grid."""
    success = False
    t = 0
    while t <= max_tile:
        url = url_og.format(t, i, j)
        try: # for some reason, wget package doesn't work at all
            subprocess.run(
                ["wget", "-P", output_directory, url],
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
            )
            success = True
            break
        except Exception:
            t += 1

    # update
    with grid_lock:
        grid[j][i] = str(t) if success else "✖"
        display_grid(grid)

def download(url_og, width, height, max_tile, out_dir=output_directory):
    grid = [['_'] * (width + 1) for _ in range(height + 1)]
    display_grid(grid)
    with concurrent.futures.ThreadPoolExecutor() as executor: # this could be so much faster with Rust
        futures = [
            executor.submit(download_tile, url_og, i, j, max_tile, grid)
            for i in range(width + 1)
            for j in range(height + 1)
        ]
        concurrent.futures.wait(futures)

# Palestine:
download("https://www.geographicus.com/mm5/graphics/00000001/zoomify/PalestineArabic-catholicpress-1948/TileGroup{}/6-{}-{}.jpg", 22, 32, 4)

# Arabian Peninsula:
# download("https://www.geographicus.com/mm5/graphics/00000001/zoomify/ArabAsiaClimate-amin-1967/TileGroup{}/6-{}-{}.jpg", 35, 27, 6)

# Москва:
# download("https://www.geographicus.com/mm5/graphics/00000001/zoomify/MoscowSixSheet-gugkussr-1982/TileGroup{}/6-{}-{}.jpg", 27, 35, 7)

# mercator:
# download("https://www.geographicus.com/mm5/graphics/00000001/zoomify/NorthPole-mercator-1595/TileGroup{}/5-{}-{}.jpg", 25, 19, 4)


# url = "https://www.geographicus.com/mm5/graphics/00000001/zoomify/PalestineArabic-catholicpress-1948/TileGroup2/6-7-12.jpg"
# cleaned_url = clean_url(url)
# print(cleaned_url)