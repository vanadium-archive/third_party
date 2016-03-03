package com.smartcodeltd.jenkinsci.plugins.buildmonitor.viewmodel;

import static hudson.model.Result.ABORTED;
import static hudson.model.Result.FAILURE;
import static hudson.model.Result.SUCCESS;
import static hudson.model.Result.UNSTABLE;

import com.smartcodeltd.jenkinsci.plugins.buildmonitor.facade.RelativeLocation;
import com.smartcodeltd.jenkinsci.plugins.buildmonitor.viewmodel.plugins.BuildAugmentor;

import org.codehaus.jackson.annotate.JsonProperty;

import hudson.model.Result;
import hudson.model.Job;
import hudson.model.Run;

import java.util.Date;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * @author Jan Molak
 */
public class JobView {
  private final Date systemTime;
  private final Job<?, ?> job;
  private final BuildAugmentor augmentor;
  private final RelativeLocation relative;

  private final static Map<Result, String> statuses = new HashMap<Result, String>() {
    {
      put(SUCCESS, "successful");
      put(UNSTABLE, "unstable");
      put(FAILURE, "failing");
      put(ABORTED, "failing"); // if someone has aborted it then something is clearly not right,
                               // right? :)
    }
  };

  public static JobView of(Job<?, ?> job) {
    return new JobView(job, new BuildAugmentor(), RelativeLocation.of(job), new Date());
  }

  public static JobView of(Job<?, ?> job, BuildAugmentor augmentor) {
    return new JobView(job, augmentor, RelativeLocation.of(job), new Date());
  }

  public static JobView of(Job<?, ?> job, RelativeLocation location) {
    return new JobView(job, new BuildAugmentor(), location, new Date());
  }

  public static JobView of(Job<?, ?> job, Date systemTime) {
    return new JobView(job, new BuildAugmentor(), RelativeLocation.of(job), systemTime);
  }

  @JsonProperty
  public String name() {
    return relative.name();
  }

  @JsonProperty
  public String shortName() {
    return relative.name().replace("vanadium-", "");
  }

  @JsonProperty
  public String url() {
    return relative.url();
  }

  private String statusOf(Result result) {
    return statuses.containsKey(result) ? statuses.get(result) : "unknown";
  }

  @JsonProperty
  public String status() {
    String status = statusOf(lastCompletedBuild().result());
    if (name().toLowerCase().indexOf("presubmit-test") > 0 &&
        (lastCompletedBuild().result() == Result.ABORTED ||
         lastCompletedBuild().result() == Result.UNSTABLE)) {
      status = "successful";
    }

    if (lastBuild().isRunning()) {
      status += " running";
    }

    if (lastCompletedBuild().isClaimed()) {
      status += " claimed";
    }

    return status;
  }

  @JsonProperty
  public String lastBuildName() {
    return lastBuild().name();
  }

  @JsonProperty
  public String lastBuildUrl() {
    return lastBuild().url();
  }

  @JsonProperty
  public String lastBuildDuration() {
    if (lastBuild().isRunning()) {
      return formatted(lastBuild().elapsedTime());
    }

    return formatted(lastBuild().duration());
  }

  @JsonProperty
  public String estimatedDuration() {
    return formatted(lastBuild().estimatedDuration());
  }

  private String formatted(Duration duration) {
    return null != duration ? duration.toString() : "";
  }

  @JsonProperty
  public int progress() {
    return lastBuild().progress();
  }

  @JsonProperty
  public boolean shouldIndicateCulprits() {
    return !isClaimed() && culprits().size() > 0;
  }

  @JsonProperty
  public Set<String> culprits() {
    Set<String> culprits = new HashSet<String>();

    BuildViewModel build = lastBuild();
    // todo: consider introducing a BuildResultJudge to keep this logic in one place
    while (!SUCCESS.equals(build.result())) {
      culprits.addAll(build.culprits());

      if (!build.hasPreviousBuild()) {
        break;
      }

      build = build.previousBuild();
    };

    return culprits;
  }

  @JsonProperty
  public boolean isClaimed() {
    return lastCompletedBuild().isClaimed();
  }

  @JsonProperty
  public String claimAuthor() {
    return lastCompletedBuild().claimant();
  }

  @JsonProperty
  public String claimReason() {
    return lastCompletedBuild().reasonForClaim();
  }

  @JsonProperty
  public boolean hasKnownFailures() {
    return lastCompletedBuild().hasKnownFailures();
  }

  @JsonProperty
  public List<String> knownFailures() {
    return lastCompletedBuild().knownFailures();
  }

  @Override
  public String toString() {
    return name();
  }


  private JobView(Job<?, ?> job, BuildAugmentor augmentor, RelativeLocation relative,
      Date systemTime) {
    this.job = job;
    this.augmentor = augmentor;
    this.systemTime = systemTime;
    this.relative = relative;
  }

  private BuildViewModel lastBuild() {
    return buildViewOf(job.getLastBuild());
  }

  private BuildViewModel lastCompletedBuild() {
    BuildViewModel previousBuild = lastBuild();
    while (previousBuild.isRunning() && previousBuild.hasPreviousBuild()) {
      previousBuild = previousBuild.previousBuild();
    }

    return previousBuild;
  }

  private BuildViewModel buildViewOf(Run<?, ?> build) {
    if (null == build) {
      return new NullBuildView();
    }

    return BuildView.of(job.getLastBuild(), augmentor, relative, systemTime);
  }
}
