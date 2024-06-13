import sensor
import image
import time
import os
import gc

# Initialize the camera sensor
sensor.reset()
sensor.set_pixformat(sensor.RGB565)  # Set pixel format to RGB565
sensor.set_framesize(sensor.VGA)     # Set frame size to VGA (640x480)
sensor.skip_frames(time=2000)        # Let the camera initialize

# Function to create a new directory for each session
def create_session_directory():
    session_num = 1
    while True:
        dir_name = "session_%03d" % session_num
        if dir_name not in os.listdir():
            os.mkdir(dir_name)
            return dir_name
        session_num += 1

# Function to capture and save an image
def capture_and_save_image(session_dir, image_count):
    try:
        img = sensor.snapshot()  # Take a picture
        img.save("%s/image_%05d.jpg" % (session_dir, image_count))  # Save the image as JPEG
        print("Saved %s/image_%05d.jpg" % (session_dir, image_count))  # Debug print
    except Exception as e:
        print("Error capturing or saving image:", e)
    finally:
        gc.collect()  # Collect garbage to free up memory

# Create a directory for the current session
session_dir = create_session_directory()

# Main loop
image_count = 0
while True:
    start_time = time.ticks_ms()
    capture_and_save_image(session_dir, image_count)
    image_count += 1
    # Ensure the delay accounts for the time taken to capture and save the image
    elapsed_time = time.ticks_diff(time.ticks_ms(), start_time)
    delay = max(0, 33 - elapsed_time)  # Approximately 30 frames per second
    time.sleep_ms(delay)
