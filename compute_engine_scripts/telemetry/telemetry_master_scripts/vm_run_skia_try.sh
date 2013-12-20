#!/bin/bash
#
# Starts the telemetry_slave_scripts/vm_run_skia_try.sh script on all slaves.
#
# The script should be run from the skia-telemetry-master GCE instance's
# /home/default/skia-repo/buildbot/compute_engine_scripts/telemetry/telemetry_master_scripts
# directory.
#
# Copyright 2013 Google Inc. All Rights Reserved.
# Author: rmistry@google.com (Ravi Mistry)


function usage() {
  cat << EOF

usage: $0 options

This script runs render pictures on SKPs with the specified patch and then runs
render pictures on SKPs without the patch. The two sets of images are then
compared and an HTML file is outputted detailing all failures.

OPTIONS:
  -h Show this message
  -e Email address of the requester
  -p The local location of the Skia patch
  -t The type of pagesets to run against. Eg: All, Filtered, 100k, 10k
  -b Which chromium build the SKPs were created with
  -a Arguments to pass to render_pictures
  -r The runid (typically requester + timestamp)
  -k Key of the App Engine Skia Try task
  -l The location of the log file
EOF
}

while getopts "he:p:t:b:a:r:k:l:" OPTION
do
  case $OPTION in
    h)
      usage
      exit 1
      ;;
    e)
      REQUESTER_EMAIL=$OPTARG
      ;;
    p)
      SKIA_PATCH_LOCATION=$OPTARG
      ;;
    t)
      PAGESETS_TYPE=$OPTARG
      ;;
    b)
      CHROMIUM_BUILD_DIR=$OPTARG
      ;;
    a)
      RENDER_PICTURES_ARGS=$OPTARG
      ;;
    r)
      RUN_ID=$OPTARG
      ;;
    k)
      APPENGINE_KEY=$OPTARG 
      ;;
    l)
      LOG_FILE=$OPTARG
      ;;
    ?)
      usage
      exit
      ;;
  esac
done

if [[ -z $SKIA_PATCH_LOCATION ]] || [[ -z $REQUESTER_EMAIL ]] || \
   [[ -z $PAGESETS_TYPE ]] || [[ -z $CHROMIUM_BUILD_DIR ]] || \
   [[ -z $RENDER_PICTURES_ARGS ]] || [[ -z $RUN_ID ]] || \
   [[ -z $APPENGINE_KEY ]] || [[ -z $LOG_FILE ]]
then
  usage
  exit 1
fi

source ../vm_config.sh
source vm_utils.sh

# Start the timer.
TIMER="$(date +%s)"

if [ -e /etc/boto.cfg ]; then
  # Move boto.cfg since it may interfere with the ~/.boto file.
  sudo mv /etc/boto.cfg /etc/boto.cfg.bak
fi

# Copy the local lua script to Google Storage.
RELATIVE_SKIA_PATCH_GS_PATH=chromium-skia-gm/telemetry/skia-tryserver/patches/$RUN_ID.patch
SKIA_PATCH_GS_LOCATION=gs://$RELATIVE_SKIA_PATCH_GS_PATH
SKIA_PATCH_GS_LINK=https://storage.cloud.google.com/$RELATIVE_SKIA_PATCH_GS_PATH
gsutil cp -a public-read $SKIA_PATCH_LOCATION $SKIA_PATCH_GS_LOCATION

# Update buildbot.
gclient sync

# Run vm_run_skia_try.sh on all the slaves.
SLAVE_LOG_FILE="/tmp/skia-try.$RUN_ID.log"
SLAVE_LOG_GS_LOCATION=gs://chromium-skia-gm/telemetry/skia-tryserver/logs/$RUN_ID
SLAVE_OUTPUT_GS_LOCATION=gs://chromium-skia-gm/telemetry/skia-tryserver/outputs/$RUN_ID
for SLAVE_NUM in $(seq 1 $NUM_SLAVES); do
  CMD="bash vm_run_skia_try.sh -n $SLAVE_NUM -p $SKIA_PATCH_GS_LOCATION -t $PAGESETS_TYPE -b $CHROMIUM_BUILD_DIR -a \"$RENDER_PICTURES_ARGS\" -r $RUN_ID -g $SLAVE_LOG_GS_LOCATION -o $SLAVE_OUTPUT_GS_LOCATION -l $SLAVE_LOG_FILE"
  ssh -f -X -o UserKnownHostsFile=/dev/null -o CheckHostIP=no \
    -o StrictHostKeyChecking=no -i /home/default/.ssh/google_compute_engine \
    -A -p 22 default@108.170.222.$SLAVE_NUM -- "source .bashrc; cd skia-repo/buildbot/compute_engine_scripts/telemetry/telemetry_slave_scripts; /home/default/depot_tools/gclient sync; $CMD > $SLAVE_LOG_FILE 2>&1"
done

# Sleep for a minute to give the slaves some time to start processing.
sleep 60

# Check to see if the slaves are done with this skia try request.
SLAVES_STILL_PROCESSING=true
while $SLAVES_STILL_PROCESSING ; do
  SLAVES_STILL_PROCESSING=false
  for SLAVE_NUM in $(seq 1 $NUM_SLAVES); do
    RET=$( is_slave_currently_executing $SLAVE_NUM SKIA-TRY.${RUN_ID} )
    if $RET; then
      echo "skia-telemetry-worker$SLAVE_NUM is still running SKIA-TRY.${RUN_ID}"
      echo "Sleeping for a minute and then retrying"
      SLAVES_STILL_PROCESSING=true
      sleep 60
      break
    else
      echo "skia-telemetry-worker$SLAVE_NUM is done processing."
    fi
  done
done

# Download JSON summary files from all the slaves.
SUMMARIES_DIR=/home/default/storage/skia-tryserver/summaries/$RUN_ID
HTML_OUTPUT_DIR=/home/default/storage/skia-tryserver/html-outputs/$RUN_ID
mkdir -p $SUMMARIES_DIR $HTML_OUTPUT_DIR
for SLAVE_NUM in $(seq 1 $NUM_SLAVES); do
  gsutil cp $SLAVE_OUTPUT_GS_LOCATION/slave$SLAVE_NUM/slave$SLAVE_NUM.json $SUMMARIES_DIR/
done
# Output HTML from the slave summaries.
cd /home/default/skia-repo/buildbot/compute_engine_scripts/telemetry/telemetry_master_scripts
RELATIVE_HTML_OUTPUT=chromium-skia-gm/telemetry/skia-tryserver/html-outputs/$RUN_ID/
ABSOLUTE_GS_LINK=https://storage.cloud.google.com/$RELATIVE_HTML_OUTPUT
python json_summary_combiner.py \
  --json_summaries_dir=$SUMMARIES_DIR \
  --output_html_dir=$HTML_OUTPUT_DIR \
  --absolute_url=$ABSOLUTE_GS_LINK
# Copy HTML output to Google Storage.
gsutil cp -R $HTML_OUTPUT_DIR/* gs://$RELATIVE_HTML_OUTPUT
# Set google.com domain ACL on the HTML files.
gsutil acl ch -g google.com:READ gs://${RELATIVE_HTML_OUTPUT}*

# End the timer.
TIMER="$(($(date +%s)-TIMER))"

# Email results to the requester and admin.
BOUNDARY=`date +%s|md5sum`
BOUNDARY=${BOUNDARY:0:32}
SLAVE_1_LOG_LINK=https://storage.cloud.google.com/chromium-skia-gm/telemetry/skia-tryserver/logs/$RUN_ID/slave1/skia-try.$RUN_ID.log
HTML_OUTPUT_LINK=${ABSOLUTE_GS_LINK}index.html
sendmail $REQUESTER_EMAIL,$ADMIN_EMAIL <<EOF
subject:Results of your Skia Try run on Cluster Telemetry ($RUN_ID)
to:$REQUESTER_EMAIL,$ADMINS
from:skia.buildbot@gmail.com
Content-Type: multipart/mixed; boundary=\"$BOUNDARY\";

This is a MIME-encapsulated message

--$BOUNDARY
Content-Type: text/html

<html>
  <head/>
  <body>
  Time taken for the Skia Try run: $TIMER seconds.<br/>
  The HTML output with differences between the base run and the patch run is <a href='$HTML_OUTPUT_LINK'>here</a>.<br/>
  If the above output is blank then please look for failures in the log file of the first slave <a href='$SLAVE_1_LOG_LINK'>here</a>.<br/><br/>
  The patch you specified is <a href='$SKIA_PATCH_GS_LINK'>here.</a><br/>
  You can schedule more runs <a href='https://skia-tree-status.appspot.com/skia-telemetry/skia_try'>here</a>.<br/><br/>
  Thanks!
  </body>
</html>

--$BOUNDARY--

EOF

# Mark this task as completed on AppEngine.
PASSWORD=`cat /home/default/skia-repo/buildbot/compute_engine_scripts/telemetry/telemetry_master_scripts/appengine_password.txt`
wget --post-data "key=$APPENGINE_KEY&patch_link=$SKIA_PATCH_GS_LINK&slave1_output_link=$SLAVE_1_LOG_LINK&html_output_link=$HTML_OUTPUT_LINK&password=$PASSWORD" "https://skia-tree-status.appspot.com/skia-telemetry/update_skia_try_task" -O /dev/null

# Delete all tmp files.
rm -rf /tmp/*${RUN_ID}*
rm -rf $SUMMARIES_DIR $HTML_OUTPUT_DIR